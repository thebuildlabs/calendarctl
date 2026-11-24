//go:build !windows

package bridge

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// bridgeSigningID is the stable identifier both the CLI and helper are signed
// with. A signature is only accepted if it carries this identifier.
const bridgeSigningID = "com.thebuildlabs.calendarctl.bridge"

// expectedTeamID is embedded by release builds via
// -X github.com/thebuildlabs/calendarctl/internal/bridge.expectedTeamID=TEAMID.
// When non-empty, a helper is trusted only if it was signed by that Developer
// ID Team, not merely by anyone using our signing identifier. Local ad-hoc
// builds leave it empty.
var expectedTeamID string

// validateOwnership rejects binaries that another user could replace or that
// are writable by their group or by everyone.
func validateOwnership(info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine bridge ownership")
	}
	if st.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("owned by uid %d, not the current user", st.Uid)
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return fmt.Errorf("group/world-writable mode %04o", perm)
	}
	return nil
}

// fileIdentity captures the device and inode so a resolved helper that is
// replaced on disk can be detected before execution.
func fileIdentity(info os.FileInfo) (uint64, uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(st.Dev), uint64(st.Ino)
}

// verifySignature requires a valid code signature with our stable signing
// identifier for every helper candidate on macOS, not just overrides. Ad-hoc
// signatures are accepted for local builds because `make bridge` signs both
// binaries ad-hoc; the helper separately pins the caller's identity (see
// native/apple/EventKitBridge.swift). Release builds additionally pin a
// Developer ID Team ID in the helper's Info.plist.
func verifySignature(path string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		// Fail closed: an unverifiable helper must never be executed.
		return fmt.Errorf("codesign not found in PATH, refusing an unverified helper: %w", err)
	}
	out, err := exec.Command(codesign, "--verify", "--strict", "--verbose=0", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// A valid signature by itself is insufficient; an attacker could sign its
	// own helper ad-hoc. Require the identifier we sign our helper with.
	info, err := exec.Command(codesign, "-dvv", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign -dvv: %s: %w", strings.TrimSpace(string(info)), err)
	}
	if !bytes.Contains(info, []byte("Identifier="+bridgeSigningID)) {
		return fmt.Errorf("bridge %s is not signed with identifier %s", path, bridgeSigningID)
	}
	if expectedTeamID != "" && !bytes.Contains(info, []byte("TeamIdentifier="+expectedTeamID)) {
		return fmt.Errorf("bridge %s is not signed by expected Team ID %s", path, expectedTeamID)
	}
	return nil
}
