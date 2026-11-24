// Package text provides small helpers for handling untrusted text before it is
// written to a terminal or embedded in diagnostics.
package text

import (
	"strings"
	"unicode"
)

// SanitizeControl removes C0/C1 control characters (including ESC) from s.
// Control characters are replaced with a single space so column widths stay
// meaningful; tabs are preserved.
//
// Calendar content (titles, notes, locations, attendees) can be influenced by
// other people via shared calendars or invitations. Passing it to a terminal
// verbatim allows escape-sequence injection such as OSC 52 clipboard writes or
// screen manipulation. Structured output (JSON/YAML) is already escaped by the
// encoders and must remain raw, so sanitization belongs at the table boundary.
func SanitizeControl(s string) string {
	if s == "" || !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
