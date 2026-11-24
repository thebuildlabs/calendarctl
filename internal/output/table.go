package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/thebuildlabs/calendarctl/internal/text"
)

// Align controls column alignment.
type Align int

const (
	AlignLeft Align = iota
	AlignRight
)

// Table is the human-readable representation of a command result.
type Table struct {
	Title    string
	Subtitle string
	Headers  []string
	Rows     [][]string
	Align    []Align
	Footer   string
	Notes    []string
}

// visibleLen ignores ANSI escape sequences when measuring width.
func visibleLen(s string) int {
	if strings.ContainsRune(s, '\x1b') {
		var b strings.Builder
		inEsc := false
		for _, r := range s {
			if r == '\x1b' {
				inEsc = true
				continue
			}
			if inEsc {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					inEsc = false
				}
				continue
			}
			b.WriteRune(r)
		}
		s = b.String()
	}
	return utf8.RuneCountInString(s)
}

func padRight(s string, width int) string {
	n := width - visibleLen(s)
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}

func padLeft(s string, width int) string {
	n := width - visibleLen(s)
	if n <= 0 {
		return s
	}
	return strings.Repeat(" ", n) + s
}

func truncate(s string, max int) string {
	if max <= 1 || visibleLen(s) <= max {
		return s
	}
	// Slice by runes: calendar titles are arbitrary Unicode and byte slicing
	// could split a multi-byte code point.
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

// WriteTable renders a compact, aligned table to stdout-style writers.
//
// Every string that can originate from calendar content is sanitized here, at
// the terminal boundary. Structured formats skip this path entirely.
func WriteTable(w io.Writer, style *Style, t Table) error {
	if style == nil {
		style = NewStyle("never", w)
	}
	var b strings.Builder

	title := text.SanitizeControl(t.Title)
	subtitle := text.SanitizeControl(t.Subtitle)
	headers := make([]string, len(t.Headers))
	for i, h := range t.Headers {
		headers[i] = text.SanitizeControl(h)
	}
	rows := make([][]string, len(t.Rows))
	for i, row := range t.Rows {
		rows[i] = make([]string, len(row))
		for j, cell := range row {
			rows[i][j] = text.SanitizeControl(cell)
		}
	}
	footer := text.SanitizeControl(t.Footer)
	notes := make([]string, len(t.Notes))
	for i, note := range t.Notes {
		notes[i] = text.SanitizeControl(note)
	}

	if title != "" {
		b.WriteString(style.Bold(title))
		b.WriteByte('\n')
	}
	if subtitle != "" {
		b.WriteString(style.Dim(subtitle))
		b.WriteByte('\n')
	}
	if title != "" || subtitle != "" {
		b.WriteByte('\n')
	}

	if len(headers) == 0 {
		for _, row := range rows {
			for _, cell := range row {
				b.WriteString(cell)
				b.WriteByte('\n')
			}
		}
	} else {
		widths := make([]int, len(headers))
		for i, h := range headers {
			widths[i] = visibleLen(h)
		}
		for _, row := range rows {
			for i, cell := range row {
				if i < len(widths) && visibleLen(cell) > widths[i] {
					widths[i] = visibleLen(cell)
				}
			}
		}
		// Cap very wide columns so long titles/IDs do not wreck the layout.
		for i := range widths {
			if widths[i] > 60 {
				widths[i] = 60
			}
		}

		// Respect narrow terminals by shrinking the widest columns first.
		termWidth := terminalWidth(w)
		if termWidth > 0 {
			total := 2
			for _, width := range widths {
				total += width + 2
			}
			for total > termWidth {
				widest, idx := -1, -1
				for i, width := range widths {
					if width > widest {
						widest, idx = width, i
					}
				}
				if idx < 0 || widest <= 8 {
					break
				}
				widths[idx]--
				total--
			}
		}

		writeRow := func(cells []string, header bool) {
			b.WriteString("  ")
			for i, h := range headers {
				if i > 0 {
					b.WriteString("  ")
				}
				cell := ""
				if i < len(cells) {
					cell = truncate(cells[i], widths[i])
				}
				if header {
					b.WriteString(style.Bold(style.Dim(strings.ToUpper(h))))
					continue
				}
				align := AlignLeft
				if i < len(t.Align) {
					align = t.Align[i]
				}
				if align == AlignRight {
					b.WriteString(padLeft(cell, widths[i]))
				} else {
					b.WriteString(padRight(cell, widths[i]))
				}
			}
			b.WriteByte('\n')
		}
		writeRow(headers, true)
		for _, row := range rows {
			writeRow(row, false)
		}
	}

	if footer != "" {
		b.WriteByte('\n')
		b.WriteString(style.Dim(footer))
		b.WriteByte('\n')
	}
	for _, note := range notes {
		b.WriteString(style.Yellow("! " + note))
		b.WriteByte('\n')
	}
	_, err := fmt.Fprint(w, b.String())
	return err
}

func terminalWidth(io.Writer) int {
	if cols := os.Getenv("COLUMNS"); cols != "" {
		var n int
		if _, err := fmt.Sscanf(cols, "%d", &n); err == nil && n > 20 {
			return n
		}
	}
	return 0
}
