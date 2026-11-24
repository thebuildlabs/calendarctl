package cli

import (
	"os"
	"testing"

	"github.com/thebuildlabs/calendarctl/internal/config"
)

func TestConfirmHonorsConfigOptOut(t *testing.T) {
	a := &App{cfg: &config.Config{ConfirmDestructive: false}}
	ok, err := a.confirm("delete?")
	if err != nil || !ok {
		t.Fatalf("confirmDestructive=false should skip prompting: ok=%t err=%v", ok, err)
	}
}

func TestConfirmRequiresExplicitYesWithoutTTY(t *testing.T) {
	fi, err := os.Stdin.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		t.Skip("stdin is an interactive terminal")
	}
	a := &App{cfg: &config.Config{ConfirmDestructive: true}}
	if _, err := a.confirm("delete?"); err == nil {
		t.Fatal("expected an error when confirmation is required without a TTY")
	}
	a.yes = true
	if ok, err := a.confirm("delete?"); err != nil || !ok {
		t.Fatalf("--yes should confirm: ok=%t err=%v", ok, err)
	}
}
