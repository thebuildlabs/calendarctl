package bridge

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPermissionTokenIsSingleUseAndPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits required")
	}
	dir := filepath.Join(t.TempDir(), "config")
	path, value, err := NewPermissionToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(value) < 32 {
		t.Fatalf("token too short: %q", value)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token mode = %04o, want 0600", perm)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("token directory mode = %04o, want 0700", perm)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != value {
		t.Fatalf("token file = %q, err = %v", b, err)
	}
	RemovePermissionToken(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token was not removed: %v", err)
	}
}
