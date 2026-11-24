package main

import (
	"bytes"
	"strings"
	"testing"

	ce "github.com/thebuildlabs/calendarctl/internal/errors"
)

func TestRenderErrorSanitizesControlSequences(t *testing.T) {
	var buf bytes.Buffer
	// Error messages can carry provider/bridge text; never let it inject
	// terminal control sequences (e.g. an OSC 52 clipboard write).
	renderError(&buf, ce.InvalidInput("test", "bad\x1b]52;c;aGVsbG8=\x07 title\x1b[2J"))
	out := buf.String()
	if strings.ContainsRune(out, '\x1b') || strings.ContainsRune(out, '\a') {
		t.Fatalf("control character survived renderError: %q", out)
	}
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "bad") || !strings.Contains(out, "title") {
		t.Fatalf("unexpected diagnostic: %q", out)
	}
}
