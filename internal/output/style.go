package output

import (
	"io"
	"os"
	"strings"
)

// Style applies ANSI colors only when the destination is a real terminal and
// the user has not disabled them.
type Style struct {
	enabled bool
}

func NewStyle(mode string, w io.Writer) *Style {
	enabled := false
	switch strings.ToLower(mode) {
	case "always":
		enabled = true
	case "never":
		enabled = false
	default: // auto
		if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || os.Getenv("CI") != "" {
			enabled = false
		} else if f, ok := w.(*os.File); ok {
			if fi, err := f.Stat(); err == nil {
				enabled = fi.Mode()&os.ModeCharDevice != 0
			}
		}
	}
	return &Style{enabled: enabled}
}

func (s *Style) Enabled() bool { return s != nil && s.enabled }

func (s *Style) apply(code, text string) string {
	if !s.Enabled() || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s *Style) Bold(v string) string   { return s.apply("1", v) }
func (s *Style) Dim(v string) string    { return s.apply("2", v) }
func (s *Style) Red(v string) string    { return s.apply("31", v) }
func (s *Style) Green(v string) string  { return s.apply("32", v) }
func (s *Style) Yellow(v string) string { return s.apply("33", v) }
func (s *Style) Blue(v string) string   { return s.apply("34", v) }
func (s *Style) Cyan(v string) string   { return s.apply("36", v) }
func (s *Style) Gray(v string) string   { return s.apply("90", v) }
