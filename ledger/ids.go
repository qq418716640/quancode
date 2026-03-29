package ledger

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewDelegationID generates a unique identifier for a delegation attempt.
func NewDelegationID() (string, error) {
	suffix, err := randomHex(8)
	if err != nil {
		return "", err
	}
	return "del_" + suffix, nil
}

// NewRunID generates a unique identifier for an entire delegate invocation
// (spanning multiple attempts in a fallback chain).
func NewRunID() (string, error) {
	suffix, err := randomHex(8)
	if err != nil {
		return "", err
	}
	return "run_" + suffix, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random hex: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
