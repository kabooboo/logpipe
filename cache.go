package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// commandChecksum derives a stable key from how logpipe was invoked (its args),
// so repeated runs of the same command reuse learned schemas.
func commandChecksum(args []string) string {
	sum := sha256.Sum256([]byte(strings.Join(args, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

// cachePath returns $HOME/.cache/logpipe/<checksum>, or "" if HOME is unknown.
func cachePath(checksum string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "logpipe", checksum)
}
