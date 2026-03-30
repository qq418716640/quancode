package job

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewJobID generates a unique job identifier with a UTC time prefix
// for natural chronological sorting by filename.
// Format: job_20260330T100000Z_a1b2c3d4e5f6g7h8
func NewJobID() (string, error) {
	ts := time.Now().UTC().Format("20060102T150405Z")
	suffix, err := randomHex(8)
	if err != nil {
		return "", fmt.Errorf("generate job id: %w", err)
	}
	return "job_" + ts + "_" + suffix, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random hex: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
