package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/thebuildlabs/calendarctl/internal/cli"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/output"
	"github.com/thebuildlabs/calendarctl/internal/text"
)

func main() {
	app := cli.NewApp()
	if err := app.Root.Execute(); err != nil {
		printError(err)
		os.Exit(ce.ExitCode(err))
	}
	os.Exit(ce.ExitOK)
}

// printError writes diagnostics to stderr with terminal-aware coloring.
func printError(err error) {
	renderError(os.Stderr, err)
}

// renderError sanitizes diagnostics before applying styling. Error messages
// may carry text from the provider (for example a bridge hint), so they must
// never be able to inject terminal control sequences.
func renderError(w io.Writer, err error) {
	style := output.NewStyle("auto", w)
	rendered := text.SanitizeControl(ce.Render(err))
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "error:"):
			lines[i] = style.Red("error:") + strings.TrimPrefix(line, "error:")
		case strings.HasPrefix(line, "hint:"):
			lines[i] = style.Dim("hint:") + strings.TrimPrefix(line, "hint:")
		}
	}
	fmt.Fprintln(w, strings.Join(lines, "\n"))
}
