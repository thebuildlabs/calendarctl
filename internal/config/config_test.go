package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAndValidate(t *testing.T) {
	c := Default()
	if c.OutputFormat != "table" || c.ConfirmDestructive != true {
		t.Fatalf("bad defaults: %+v", c)
	}
	c.OutputFormat = "JSON"
	c.Color = "WEIRD"
	c.Validate()
	if c.OutputFormat != "json" {
		t.Errorf("output format = %q", c.OutputFormat)
	}
	if c.Color != "auto" {
		t.Errorf("color = %q", c.Color)
	}
}

func TestInitAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	t.Setenv("CALENDARCTL_CONFIG", path)

	if _, err := Init(false); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(false); err == nil {
		t.Fatal("expected error on duplicate init")
	}
	if _, err := Init(true); err != nil {
		t.Fatal(err)
	}
	c, loadedPath, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loadedPath != path {
		t.Fatalf("path = %q", loadedPath)
	}
	if c.DefaultMeetingMinutes != 30 {
		t.Fatalf("meeting minutes = %d", c.DefaultMeetingMinutes)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config file must not be world/group readable: %v", info.Mode())
	}
}

func TestEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	t.Setenv("CALENDARCTL_CONFIG", path)
	t.Setenv("CALENDARCTL_OUTPUT_FORMAT", "json")
	t.Setenv("CALENDARCTL_TIMEZONE", "UTC")
	t.Setenv("CALENDARCTL_CONFIRM_DESTRUCTIVE", "false")

	c, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.OutputFormat != "json" || c.Timezone != "UTC" || c.ConfirmDestructive {
		t.Fatalf("env was not applied: %+v", c)
	}
}

func TestLoadIgnoresMissingFile(t *testing.T) {
	t.Setenv("CALENDARCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	c, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "auto" {
		t.Fatalf("provider = %q", c.Provider)
	}
}
