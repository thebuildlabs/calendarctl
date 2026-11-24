package cli

import (
	"os"
	"testing"
)

func TestBridgeOverrideRequiresExplicitOptIn(t *testing.T) {
	fi, err := os.Stdin.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		t.Skip("stdin is an interactive terminal")
	}
	a := &App{}
	if err := a.authorizeBridgeOverride("/tmp/attacker-helper"); err == nil {
		t.Fatal("expected an error without an explicit opt-in")
	}
	// --yes must not authorize a bridge override.
	a.yes = true
	if err := a.authorizeBridgeOverride("/tmp/attacker-helper"); err == nil {
		t.Fatal("--yes must not authorize a bridge override")
	}
	// Only the dedicated flag permits non-interactive use.
	a.allowBridgeOverride = true
	if err := a.authorizeBridgeOverride("/tmp/attacker-helper"); err != nil {
		t.Fatalf("--allow-bridge-override should authorize the override: %v", err)
	}
}
