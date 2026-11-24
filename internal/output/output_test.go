package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableAlignmentAndTruncation(t *testing.T) {
	var buf bytes.Buffer
	err := WriteTable(&buf, NewStyle("never", &buf), Table{
		Title:   "TEST",
		Headers: []string{"NAME", "VALUE"},
		Rows:    [][]string{{"A", "short"}, {"Longer name", "x"}},
		Footer:  "2 rows",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"TEST", "NAME", "VALUE", "Longer name", "2 rows"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderFormats(t *testing.T) {
	data := map[string]any{"ok": true, "count": 2}
	for _, format := range []string{"json", "yaml"} {
		var buf bytes.Buffer
		r := New(format, false, &buf)
		if err := r.Render(data, Table{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "ok") || !strings.Contains(buf.String(), "2") {
			t.Errorf("%s: unexpected output %q", format, buf.String())
		}
	}
	var buf bytes.Buffer
	r := New("json", true, &buf)
	if err := r.Render(data, Table{}); err != nil || buf.Len() != 0 {
		t.Fatalf("quiet mode should emit nothing: %q %v", buf.String(), err)
	}
}

func TestTableSanitizesControlSequences(t *testing.T) {
	var buf bytes.Buffer
	// A crafted calendar title attempting an OSC 52 clipboard write.
	malicious := "Standup\x1b]52;c;aGVsbG8=\x07\x1b[2J"
	err := WriteTable(&buf, NewStyle("never", &buf), Table{
		Title:   "EVENTS",
		Headers: []string{"TITLE"},
		Rows:    [][]string{{malicious}},
		Footer:  "notices\x1b[0K",
		Notes:   []string{"bad\x1b]52;c;eA==\x07"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.ContainsRune(out, '\x1b') {
		t.Fatalf("control character survived sanitization: %q", out)
	}
	if !strings.Contains(out, "Standup") {
		t.Fatalf("legible text was lost: %q", out)
	}
}

func TestPrintSanitizesControlSequences(t *testing.T) {
	var buf bytes.Buffer
	r := New("table", false, &buf)
	// Success messages embed calendar titles, which may come from shared
	// calendars. They must be safe at the terminal boundary.
	if err := r.Print("Deleted event Standup\x1b]52;c;aGVsbG8=\x07\x1b[2J"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.ContainsRune(out, '\x1b') {
		t.Fatalf("control character survived Print: %q", out)
	}
	if !strings.Contains(out, "Deleted event Standup") {
		t.Fatalf("legible text was lost: %q", out)
	}
}

func TestStyleModes(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle("never", &buf)
	if s.Enabled() {
		t.Fatal("never should disable color")
	}
	s = NewStyle("always", &buf)
	if !s.Enabled() || !strings.Contains(s.Red("x"), "\x1b[31m") {
		t.Fatal("always should enable color")
	}
}
