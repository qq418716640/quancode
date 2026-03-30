package job

import (
	"os"
	"sync"
)

// DefaultMaxOutputBytes is the default maximum size for job output files (50MB).
const DefaultMaxOutputBytes = 50 * 1024 * 1024

// CappedFile is an io.Writer that writes to a file up to a maximum size.
// Once the cap is reached, further writes are silently discarded.
// It is safe for concurrent use.
type CappedFile struct {
	mu      sync.Mutex
	file    *os.File
	written int64
	cap     int64
}

// NewCappedFile creates a CappedFile that writes to the given path.
// If maxBytes <= 0, DefaultMaxOutputBytes is used.
func NewCappedFile(path string, maxBytes int64) (*CappedFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxOutputBytes
	}
	return &CappedFile{file: f, cap: maxBytes}, nil
}

// Write implements io.Writer. Writes that would exceed the cap are truncated.
func (c *CappedFile) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	remaining := c.cap - c.written
	if remaining <= 0 {
		return len(p), nil // discard silently
	}

	toWrite := p
	if int64(len(p)) > remaining {
		toWrite = p[:remaining]
	}

	n, err := c.file.Write(toWrite)
	c.written += int64(n)
	if err != nil {
		return n, err
	}

	// Report full len(p) consumed to avoid short-write errors in callers.
	return len(p), nil
}

// Truncated reports whether any writes were discarded due to the size cap.
func (c *CappedFile) Truncated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written >= c.cap
}

// Close closes the underlying file.
func (c *CappedFile) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.file.Close()
}
