package cli

import (
	"bufio"
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thebuildlabs/calendarctl/internal/bridge"
	"github.com/thebuildlabs/calendarctl/internal/calendar"
	"github.com/thebuildlabs/calendarctl/internal/config"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/logging"
	"github.com/thebuildlabs/calendarctl/internal/output"
	"github.com/thebuildlabs/calendarctl/internal/text"
)

// ensurePermissions performs the native permission request and, on macOS,
// falls back to launching the helper app through LaunchServices when the
// system dialog is refused to a bare CLI child process.
func (a *App) ensurePermissions(ctx context.Context, allowAppLaunch bool) error {
	provider := a.Service().Provider()
	status, err := provider.RequestPermissions(ctx)

	if allowAppLaunch && (err != nil || status == nil || status.State == "notDetermined") &&
		runtime.GOOS == "darwin" {
		if app := appBundlePath(provider); app != "" {
			// The helper's --request-permissions path is gated by a one-time
			// token. Create it in the (0700) config directory so a bare
			// `open ... --request-permissions` cannot trigger the TCC dialog.
			tokenPath, token, tokenErr := bridge.NewPermissionToken(filepath.Dir(a.cfgPath))
			if tokenErr != nil {
				a.log.Warn("permission token creation failed", logging.F("error", tokenErr.Error()))
			} else {
				launch := exec.Command("open", "-W", app, "--args",
					"--request-permissions", "--token-file", tokenPath, "--token", token)
				if out, launchErr := launch.CombinedOutput(); launchErr != nil {
					a.log.Warn("permission app launch failed", logging.F("bytes", len(out)))
				}
				bridge.RemovePermissionToken(tokenPath)
			}
			status, err = provider.CheckPermissions(ctx)
		}
	}

	if err != nil {
		var typed *ce.Error
		if !stderrors.As(err, &typed) || typed.Code != ce.CodePermission {
			return err
		}
	}
	if status == nil || !status.IsReadable() {
		hint := "Run `calendarctl permissions request` from a real Terminal window, " +
			"then re-run `calendarctl init`. Alternatively, grant access in " +
			"System Settings → Privacy & Security → Calendars (look for calendarctl Event Kit Bridge) " +
			"and re-run `calendarctl permissions check`."
		return ce.Permission("init.permissions",
			"calendar access was not granted; macOS did not display a permission prompt", hint)
	}
	return nil
}

// initCommand is the single onboarding command: permissions + config + default calendar.
func (a *App) initCommand() *cobra.Command {
	var calendarRef, timezone, format string
	var force, skipPermissions bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up calendarctl: permissions, config and default calendar",
		Long: "One-command onboarding. Requests calendar access, creates the config file,\n" +
			"and selects a default calendar. Run it from a real Terminal window on macOS.",
		Example: `  calendarctl init
  calendarctl init --calendar Work --timezone Asia/Kolkata
  calendarctl init --force --skip-permissions`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := a.Renderer()

			if !skipPermissions {
				if err := a.ensurePermissions(cmd.Context(), true); err != nil {
					return err
				}
			}

			// Only create/overwrite config when explicitly requested or absent.
			_, statErr := os.Stat(a.cfgPath)
			if force || os.IsNotExist(statErr) {
				if err := config.SaveTo(a.cfgPath, a.cfg); err != nil {
					return err
				}
			}

			calendars, err := a.Service().Provider().ListCalendars(cmd.Context())
			if err != nil {
				return err
			}
			var writable []calendar.Calendar
			for _, c := range calendars {
				if c.Writable {
					writable = append(writable, c)
				}
			}

			chosen := a.cfg.DefaultCalendar
			if calendarRef != "" {
				c, err := a.Service().ResolveCalendar(cmd.Context(), calendarRef)
				if err != nil {
					return err
				}
				if !c.Writable {
					// Use an opaque message: titles are private calendar payloads
					// and must not be embedded in diagnostics.
					return ce.WithHint(
						ce.InvalidInput("init", "the selected calendar is read-only"),
						"choose a writable calendar with --calendar")
				}
				chosen = c.Title
			} else if chosen == "" && len(writable) > 0 {
				chosen, err = a.pickCalendar(writable)
				if err != nil {
					return err
				}
			}
			if timezone != "" {
				if _, err := time.LoadLocation(timezone); err != nil {
					return ce.InvalidInput("init", "invalid timezone "+timezone)
				}
				a.cfg.Timezone = timezone
			}
			if format != "" {
				a.cfg.OutputFormat = strings.ToLower(format)
				a.cfg.Validate()
			}
			if chosen != "" {
				a.cfg.DefaultCalendar = chosen
			}
			if err := config.SaveTo(a.cfgPath, a.cfg); err != nil {
				return err
			}

			if r.Format == "table" {
				lines := []string{
					"calendarctl is ready.",
					"  Config:  " + a.cfgPath,
					"  Calendar: " + orDashWithFallback(chosen, "(first writable)"),
					"  Timezone: " + orDashWithFallback(a.cfg.Timezone, "system local"),
					"Next: calendarctl event list --today",
				}
				return success(r, strings.Join(lines, "\n"))
			}
			return r.Render(map[string]any{
				"ready":           true,
				"configPath":      a.cfgPath,
				"defaultCalendar": chosen,
				"timezone":        a.cfg.Timezone,
				"outputFormat":    a.cfg.OutputFormat,
			}, output.Table{})
		},
	}
	cmd.Flags().StringVar(&calendarRef, "calendar", "", "default calendar (ID or name)")
	cmd.Flags().StringVar(&timezone, "timezone", "", "default IANA timezone")
	cmd.Flags().StringVar(&format, "format", "", "default output format: table, json, yaml")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file")
	cmd.Flags().BoolVar(&skipPermissions, "skip-permissions", false, "skip the permission request")
	return cmd
}

func (a *App) pickCalendar(calendars []calendar.Calendar) (string, error) {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return calendars[0].Title, nil
	}
	fmt.Fprintln(os.Stderr, "Choose a default calendar:")
	for i, c := range calendars {
		// Calendar titles come from shared/subscribed calendars; sanitize them
		// before writing to the terminal.
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, text.SanitizeControl(c.Title))
	}
	fmt.Fprint(os.Stderr, "Number [1]: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err.Error() != "EOF" {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return calendars[0].Title, nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(calendars) {
		return "", ce.InvalidInput("init", "invalid calendar selection")
	}
	return calendars[n-1].Title, nil
}
