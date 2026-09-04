package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const yaraUserAgent = "yara"

const opencodeSessionHeader = "X-Opencode-Session"

const opencodeSessionLength = 8

// SessionForJob derives a stable, opaque session ID from a job ID so OpenCode
// can group a job's requests (including retries, which reuse the same job) for
// prompt-cache optimization. The hash keeps internal IDs out of the header.
func SessionForJob(jobID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(jobID)))
	return hex.EncodeToString(sum[:])[:opencodeSessionLength]
}
