package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ce "github.com/thebuildlabs/calendarctl/internal/errors"
)

// Resolution describes the helper binary that was selected and how.
type Resolution struct {
	Path     string
	Source   string // "override" (flag/env/config) or "bundled" (discovered)
	Warnings []string

	// dev/ino capture the file identity at validation time so a later call can
	// detect a swap of the helper binary before executing it.
	dev uint64
	ino uint64
}

// Revalidate re-checks that the resolved binary is still the exact file that
// was validated and is still safe to execute. It is called immediately before
// every invocation, closing the window between Resolve and exec.
func (r *Resolution) Revalidate() error {
	if r == nil || r.Path == "" {
		return fmt.Errorf("empty bridge resolution")
	}
	info, err := os.Lstat(r.Path)
	if err != nil {
		return fmt.Errorf("stat bridge: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bridge was replaced with a symlink")
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("bridge is not an executable file")
	}
	if err := validateOwnership(info); err != nil {
		return err
	}
	dev, ino := fileIdentity(info)
	if dev != r.dev || ino != r.ino {
		return fmt.Errorf("bridge file changed after validation")
	}
	return verifySignature(r.Path)
}

// ExplicitOverride returns the helper path the caller explicitly selected, in
// priority order: flag, config file, environment. An empty string means the
// bundled helper should be discovered normally.
func ExplicitOverride(flagPath, configPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if configPath != "" {
		return configPath
	}
	return os.Getenv("CALENDARCTL_BRIDGE")
}

// HasOverride reports whether the caller explicitly selected a bridge path.
func HasOverride(flagPath, configPath string) bool {
	return ExplicitOverride(flagPath, configPath) != ""
}

// ResolvePath finds the helper binary using, in order, an explicit override,
// CALENDARCTL_BRIDGE, the directory of the running executable, and common
// development build locations.
//
// Callers that need to surface warnings about overrides should use Resolve.
func ResolvePath(override string) (string, error) {
	r, err := Resolve(override)
	if err != nil {
		return "", err
	}
	return r.Path, nil
}

// Resolve finds and validates the helper binary. Every candidate must be owned
// by the current user, must not be group/world-writable, and on macOS must carry
// a valid code signature with our stable signing identifier. Bundled helpers
// are discovered only relative to the running executable — never relative to
// the current working directory — so a same-user process cannot substitute a
// helper by changing directory. Overrides (--bridge, CALENDARCTL_BRIDGE, config
// bridgePath) are additionally reported as warnings to the user.
func Resolve(override string) (*Resolution, error) {
	type candidate struct {
		path   string
		source string
	}
	var candidates []candidate
	add := func(p, source string) {
		if p != "" {
			candidates = append(candidates, candidate{path: p, source: source})
		}
	}
	add(override, "override")
	add(os.Getenv("CALENDARCTL_BRIDGE"), "override")

	if exe, err := os.Executable(); err == nil {
		// Prefer the bundled binary: macOS TCC treats it as a responsible app.
		// Only executable-relative paths are used; CWD-relative fallbacks let a
		// same-user actor swap the helper by changing directory.
		add(filepath.Join(filepath.Dir(exe), "EventKitBridge.app", "Contents", "MacOS", "EventKitBridge"), "bundled")
		add(filepath.Join(filepath.Dir(exe), "EventKitBridge"), "bundled")
	}

	seen := map[string]bool{}
	var looked []string
	for _, c := range candidates {
		if seen[c.path] {
			continue
		}
		seen[c.path] = true
		looked = append(looked, c.path)

		info, err := os.Lstat(c.path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		// Never execute through an unchecked symlink: validate the target.
		realPath, err := filepath.EvalSymlinks(c.path)
		if err != nil {
			continue
		}
		realInfo, err := os.Stat(realPath)
		if err != nil || realInfo.IsDir() || realInfo.Mode()&0o111 == 0 {
			continue
		}
		if err := validateOwnership(realInfo); err != nil {
			// A bad override is a deliberate choice; fail loudly instead of
			// silently falling through to a different binary.
			if c.source == "override" {
				return nil, ce.Unavailable("bridge", fmt.Sprintf("unsafe bridge path %s: %v", realPath, err))
			}
			continue
		}
		abs, _ := filepath.Abs(realPath)
		// A helper without a valid signature is never executed, regardless of
		// how it was discovered. Bundled candidates are skipped; overrides fail
		// loudly because they were explicitly chosen by the user.
		if err := verifySignature(abs); err != nil {
			if c.source == "override" {
				return nil, ce.Unavailable("bridge", fmt.Sprintf("bridge override %s failed signature verification: %v", abs, err))
			}
			continue
		}
		dev, ino := fileIdentity(realInfo)
		r := &Resolution{Path: abs, Source: c.source, dev: dev, ino: ino}
		if c.source == "override" {
			r.Warnings = append(r.Warnings,
				fmt.Sprintf("using bridge override %s instead of the bundled helper", abs))
		}
		return r, nil
	}
	return nil, ce.Unavailable("bridge", fmt.Sprintf(
		"EventKit bridge not found or unsafe (looked for %s). Build it with `make bridge` or set CALENDARCTL_BRIDGE.",
		strings.Join(looked, ", ")))
}
