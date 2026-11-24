package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/text"
)

// authorizeBridgeOverride gates an explicitly chosen helper binary. Every
// calendar payload (titles, notes, locations, attendees, IDs) is sent to the
// resolved binary, so a caller-chosen path requires a deliberate, explicit
// opt-in. The warning is written straight to stderr so it is visible even in
// --quiet, --json, or non-interactive modes.
//
// In non-interactive environments the dedicated --allow-bridge-override flag
// is required. --yes is intentionally NOT accepted here: it is the opt-out for
// destructive commands, not a security decision about a helper binary.
func (a *App) authorizeBridgeOverride(path string) error {
	fmt.Fprintf(os.Stderr,
		"warning: EventKit bridge override in use (%s); all calendar data will be sent to this binary\n",
		text.SanitizeControl(path))

	if a.allowBridgeOverride {
		return nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return ce.WithHint(
			ce.InvalidInput("bridge", "bridge override requires explicit approval in non-interactive mode"),
			"re-run interactively, or pass --allow-bridge-override")
	}

	fmt.Fprint(os.Stderr, "Send calendar data to this bridge? [y/N]: ")
	line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
	if readErr != nil && readErr.Error() != "EOF" {
		return readErr
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	default:
		return ce.Cancelled("bridge")
	}
}
