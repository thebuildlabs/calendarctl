package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thebuildlabs/calendarctl/internal/config"
	"github.com/thebuildlabs/calendarctl/internal/output"
)

func (a *App) configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long:  "Configuration is stored as YAML. Credentials and account tokens are never stored in this file.",
	}
	initCmd := &cobra.Command{
		Use:         "init",
		Short:       "Create a default configuration file",
		Annotations: map[string]string{"skipProvider": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, _ := cmd.Flags().GetBool("force")
			target := a.configFlag
			if target == "" {
				target = a.cfgPath
			}
			path, err := config.InitTo(target, force)
			if err != nil {
				return err
			}
			return a.Renderer().Print(fmt.Sprintf("Created config at %s", path))
		},
	}
	initCmd.Flags().Bool("force", false, "overwrite an existing config")

	cmd.AddCommand(initCmd)
	cmd.AddCommand(&cobra.Command{
		Use:         "show",
		Short:       "Print the effective configuration",
		Annotations: map[string]string{"skipProvider": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.cfg == nil {
				if err := a.initBase(); err != nil {
					return err
				}
			}
			r := a.Renderer()
			rows := [][]string{
				{"Path", a.cfgPath},
				{"Provider", a.cfg.Provider},
				{"Default calendar", orDash(a.cfg.DefaultCalendar)},
				{"Timezone", orDashWithFallback(a.cfg.Timezone, "system local")},
				{"Output format", a.cfg.OutputFormat},
				{"Color", a.cfg.Color},
				{"Confirm destructive", fmt.Sprintf("%t", a.cfg.ConfirmDestructive)},
				{"Date format", a.cfg.DateFormat},
				{"Time format", a.cfg.TimeFormat},
				{"Working hours", a.cfg.WorkingHoursStart + " - " + a.cfg.WorkingHoursEnd},
				{"Default meeting", fmt.Sprintf("%d min", a.cfg.DefaultMeetingMinutes)},
				{"Default reminders", fmt.Sprintf("%v", a.cfg.DefaultReminderMins)},
			}
			return r.Render(a.cfg, output.Table{
				Title:   "CONFIGURATION",
				Headers: []string{"KEY", "VALUE"},
				Rows:    rows,
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:         "path",
		Short:       "Print the configuration file path",
		Annotations: map[string]string{"skipProvider": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.cfgPath == "" {
				if err := a.initBase(); err != nil {
					return err
				}
			}
			_, err := fmt.Fprintln(os.Stdout, a.cfgPath)
			return err
		},
	})
	return cmd
}

func orDashWithFallback(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
