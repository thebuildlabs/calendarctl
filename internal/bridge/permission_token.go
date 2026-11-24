package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// permissionTokenBytes is the entropy in the one-time token that authorizes the
// helper's --request-permissions launcher path. The token is consumed (deleted)
// by the helper after a single use.
const permissionTokenBytes = 32

// NewPermissionToken creates a one-time token file in dir and returns its path
// and value. The directory is created with 0700 and the file with 0600, so only
// the current user can read the token. Callers pass both values to the helper
// as command-line arguments when launching it through LaunchServices.
func NewPermissionToken(dir string) (path, value string, err error) {
	if dir == "" {
		return "", "", fmt.Errorf("permission token directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create permission token directory: %w", err)
	}
	var seed [permissionTokenBytes]byte
	if _, err := rand.Read(seed[:]); err != nil {
		// Never fall back to a predictable token.
		return "", "", fmt.Errorf("generate permission token: %w", err)
	}
	value = hex.EncodeToString(seed[:])
	path = filepath.Join(dir, fmt.Sprintf(".permission-token-%d", os.Getpid()))
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return "", "", fmt.Errorf("write permission token: %w", err)
	}
	return path, value, nil
}

// RemovePermissionToken removes a token that was not consumed by the helper
// (for example when the app launch failed). Missing files are not an error.
func RemovePermissionToken(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
