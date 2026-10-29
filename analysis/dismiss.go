package analysis

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
)

// DismissalKey identifies a finding the user dismissed as not a real problem:
// its rule and the SHA-256 of its line with surrounding space trimmed. It
// keeps no path, so it follows the line when a file moves, and it lapses when
// the line changes. Neither the line nor any value in it is stored.
func DismissalKey(rule string, line []byte) string {
	sum := sha256.Sum256(bytes.TrimSpace(line))
	return rule + ":" + hex.EncodeToString(sum[:])
}
