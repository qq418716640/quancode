package ui

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// isTTY is cached at init time since stderr's terminal status
// does not change during program execution.
var isTTY = term.IsTerminal(int(os.Stderr.Fd()))

// IsTTY reports whether stderr is connected to a terminal.
func IsTTY() bool {
	return isTTY
}

// Spinner displays an animated spinner on stderr while a long-running
// operation is in progress. It degrades gracefully to a static message
// when stderr is not a TTY.
type Spinner struct {
	message string
	stop    chan struct{}
	done    chan struct{}
	start   time.Time
}

// NewSpinner creates and starts a spinner with the given message.
func NewSpinner(message string) *Spinner {
	s := &Spinner{
		message: message,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		start:   time.Now(),
	}
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer close(s.done)

	if !isTTY {
		// Non-TTY: print a static line and wait for stop
		fmt.Fprintf(os.Stderr, "[quancode] %s\n", s.message)
		<-s.stop
		return
	}

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	frame := 0
	for {
		select {
		case <-s.stop:
			// Clear the spinner line
			fmt.Fprintf(os.Stderr, "\r\033[K")
			return
		case <-ticker.C:
			elapsed := time.Since(s.start).Truncate(time.Second)
			fmt.Fprintf(os.Stderr, "\r\033[K[quancode] %s %s (%s)",
				spinnerFrames[frame%len(spinnerFrames)], s.message, elapsed)
			frame++
		}
	}
}

// Stop halts the spinner animation and clears the line.
// Safe to call multiple times.
func (s *Spinner) Stop() {
	select {
	case <-s.stop:
		// Already stopped
	default:
		close(s.stop)
	}
	<-s.done
}
