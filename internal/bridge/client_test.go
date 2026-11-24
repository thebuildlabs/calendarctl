package bridge

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	ce "github.com/thebuildlabs/calendarctl/internal/errors"
)

func writeFakeBridge(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "bridge")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat >/dev/null\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCallSuccess(t *testing.T) {
	path := writeFakeBridge(t, `printf '%s' '{"ok":true,"data":{"value":42}}'`)
	c := New(path, nil)
	var out struct {
		Value int `json:"value"`
	}
	if err := c.Call(context.Background(), CmdPing, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Value != 42 {
		t.Fatalf("value = %d", out.Value)
	}
}

func TestCallMapsWireError(t *testing.T) {
	path := writeFakeBridge(t, `printf '%s' '{"ok":false,"error":{"code":"permission_denied","message":"denied"}}'`)
	c := New(path, nil)
	err := c.Call(context.Background(), CmdCheckPermissions, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	e, ok := err.(*ce.Error)
	if !ok || e.Code != ce.CodePermission {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestCallDeliversHandshakeOutOfBand(t *testing.T) {
	// The helper reads the nonce from fd 3 and requires it in the JSON request.
	path := writeFakeBridge(t, `nonce=$(cat <&3); printf '{"ok":true,"data":{"nonce":"%s"}}' "$nonce"`)
	c := New(path, nil)
	var out struct {
		Nonce string `json:"nonce"`
	}
	if err := c.Call(context.Background(), CmdPing, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Nonce != c.nonce || out.Nonce == "" {
		t.Fatalf("handshake mismatch: got %q, want %q", out.Nonce, c.nonce)
	}
}

func TestFrameRequest(t *testing.T) {
	payload := []byte(`{"command":"ping","nonce":"abc"}`)
	frame := frameRequest(payload)
	if len(frame) != 4+len(payload) {
		t.Fatalf("frame length = %d", len(frame))
	}
	if got := binary.BigEndian.Uint32(frame[:4]); got != uint32(len(payload)) {
		t.Fatalf("prefix = %d, want %d", got, len(payload))
	}
	if string(frame[4:]) != string(payload) {
		t.Fatalf("body = %q", frame[4:])
	}
}

func TestVerifySignatureFailsClosedWithoutCodesign(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign check is macOS-only")
	}
	t.Setenv("PATH", t.TempDir()) // hide /usr/bin/codesign
	if err := verifySignature(filepath.Join(t.TempDir(), "helper")); err == nil {
		t.Fatal("expected an error when codesign is unavailable on macOS")
	}
}

func TestResolvedClientDetectsHelperSwap(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("fake helper cannot pass macOS codesign verification")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"ok\":true,\"data\":{\"value\":1}}'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	dev, ino := fileIdentity(info)
	c := NewResolved(&Resolution{Path: path, Source: "bundled", dev: dev, ino: ino}, nil)

	var out struct {
		Value int `json:"value"`
	}
	if err := c.Call(context.Background(), CmdPing, nil, &out); err != nil || out.Value != 1 {
		t.Fatalf("first call: value=%d err=%v", out.Value, err)
	}

	// Replace the helper with a different file (new inode) while keeping the
	// same absolute path — the classic resolve→exec TOCTOU swap.
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(replacement, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	out.Value = 0
	if err := c.Call(context.Background(), CmdPing, nil, &out); err == nil {
		t.Fatal("expected an error after the helper file was replaced")
	}
}

func TestHasOverride(t *testing.T) {
	t.Setenv("CALENDARCTL_BRIDGE", "")
	if HasOverride("", "") {
		t.Fatal("no override expected")
	}
	if !HasOverride("/flag/path", "") {
		t.Fatal("flag override not detected")
	}
	if !HasOverride("", "/config/path") {
		t.Fatal("config override not detected")
	}
	t.Setenv("CALENDARCTL_BRIDGE", "/env/path")
	if !HasOverride("", "") {
		t.Fatal("env override not detected")
	}
	if got := ExplicitOverride("", ""); got != "/env/path" {
		t.Fatalf("env override = %q", got)
	}
	if got := ExplicitOverride("/flag", "/config"); got != "/flag" {
		t.Fatalf("flag override should win: %q", got)
	}
}

func TestResolvePath(t *testing.T) {
	if _, err := ResolvePath(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected unavailable error")
	}
}

func TestResolveRejectsUnsafeOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits required")
	}
	path := filepath.Join(t.TempDir(), "bridge")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o777); err != nil {
		t.Fatal(err)
	}
	// Set the mode explicitly: umask can strip group/world bits on create.
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(path); err == nil {
		t.Fatal("expected an error for a world-writable bridge override")
	}
}

func TestResolveRejectsSymlinkToUnsafeTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks/permission bits required")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o777); err != nil {
		t.Fatal(err)
	}
	// Set the mode explicitly: umask can strip group/world bits on create.
	if err := os.Chmod(target, 0o777); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "bridge")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(link); err == nil {
		t.Fatal("expected an error for a symlink to a world-writable target")
	}
}
