package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/thebuildlabs/calendarctl/internal/calendar"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/output"
	"github.com/thebuildlabs/calendarctl/internal/text"
)

// confirm asks before destructive actions. Non-interactive environments must
// pass --yes explicitly; this prevents accidental scripted deletion. Setting
// confirmDestructive: false (or CALENDARCTL_CONFIRM_DESTRUCTIVE=false) is an
// explicit, user-owned opt-out and is honored here so `config show` reflects
// the effective behavior.
func (a *App) confirm(message string) (bool, error) {
	if a.yes {
		return true, nil
	}
	if a.cfg != nil && !a.cfg.ConfirmDestructive {
		return true, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false, &ce.Error{
			Code:    ce.CodeInvalidInput,
			Op:      "confirm",
			Message: "confirmation required but stdin is not interactive",
			Hint:    "pass --yes to confirm non-interactively",
		}
	}
	// Confirmation labels can contain calendar titles from shared calendars.
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", text.SanitizeControl(message))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err.Error() != "EOF" {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		return true, nil
	}
	return false, nil
}

func formatClock(t time.Time, cfgTimeFormat string) string {
	if cfgTimeFormat == "" {
		cfgTimeFormat = "15:04"
	}
	return t.Format(cfgTimeFormat)
}

// formatEventTime renders the TIME column for an event.
func formatEventTime(e calendar.Event, cfgTimeFormat string) string {
	if e.AllDay {
		return "all-day"
	}
	return formatClock(e.Start, cfgTimeFormat) + " - " + formatClock(e.End, cfgTimeFormat)
}

// formatEventDate renders the date for an event (used in multi-day listings).
func formatEventDate(e calendar.Event) string {
	return e.Start.Format("Mon Jan 02")
}

func success(r *output.Renderer, message string) error {
	return r.Print(message)
}

func ceInvalid(message string) error { return ce.InvalidInput("cli", message) }
func ceCancelled() error             { return ce.Cancelled("cli") }

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12] + "…"
	}
	return id
}
