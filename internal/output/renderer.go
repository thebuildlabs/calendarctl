// Package output renders command results as tables, JSON or YAML.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/thebuildlabs/calendarctl/internal/text"
)

// Renderer selects a format and writes results to the appropriate stream.
type Renderer struct {
	Format string
	Quiet  bool
	Writer io.Writer
	Style  *Style
}

func New(format string, quiet bool, w io.Writer) *Renderer {
	if w == nil {
		w = os.Stdout
	}
	return &Renderer{
		Format: format,
		Quiet:  quiet,
		Writer: w,
		Style:  NewStyle("auto", w),
	}
}

// Render writes data in the configured format. For table format, tbl is used;
// for structured formats, data is marshaled.
func (r *Renderer) Render(data any, tbl Table) error {
	if r.Quiet {
		return nil
	}
	switch r.Format {
	case "json":
		return writeJSON(r.Writer, data)
	case "yaml":
		return writeYAML(r.Writer, data)
	default:
		return WriteTable(r.Writer, r.Style, tbl)
	}
}

// Print writes a plain human message (suppressed in quiet/structured modes).
//
// Messages may embed calendar-controlled text (event and calendar titles).
// Sanitize here so every human-facing message is safe at the terminal boundary,
// including the success/confirmation paths that do not go through WriteTable.
func (r *Renderer) Print(msg string) error {
	if r.Quiet || r.Format != "table" {
		return nil
	}
	_, err := fmt.Fprintln(r.Writer, text.SanitizeControl(msg))
	return err
}

func writeJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func writeYAML(w io.Writer, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
