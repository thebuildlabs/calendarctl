//go:build windows

package bridge

import "os"

// Windows does not expose POSIX ownership bits; the helper is macOS-only, so
// there is nothing to enforce here.
func validateOwnership(os.FileInfo) error { return nil }

func fileIdentity(os.FileInfo) (uint64, uint64) { return 0, 0 }

func verifySignature(string) error { return nil }
