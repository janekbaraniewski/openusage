package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// spinner is a tiny stderr progress indicator for slow CLI work (log parsing,
// telemetry collection, provider polls). It writes only to stderr and only when
// stderr is a terminal, so piped/redirected stdout (tables, JSON) is never
// affected and non-interactive runs stay quiet.
type spinner struct {
	stopCh chan struct{}
	done   chan struct{}
	once   sync.Once
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	spinnerInterval = 90 * time.Millisecond
	ansiOrange256   = "\033[38;5;208m"
	ansiDim         = "\033[2m"
	ansiClear       = "\033[0m"
)

// startSpinner begins animating with the given message. If stderr is not a
// terminal it returns a no-op spinner. Always pair with stop().
func startSpinner(message string) *spinner {
	s := &spinner{stopCh: make(chan struct{}), done: make(chan struct{})}
	if !stderrIsTerminal() {
		close(s.done)
		return s
	}
	go s.run(message)
	return s
}

func (s *spinner) run(message string) {
	defer close(s.done)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			frame := spinnerFrames[i%len(spinnerFrames)]
			i++
			fmt.Fprintf(os.Stderr, "\r%s%s%s %s%s%s", ansiOrange256, frame, ansiClear, ansiDim, message, ansiClear)
		}
	}
}

// stop ends the animation and clears the spinner line. Safe to call once.
func (s *spinner) stop() {
	s.once.Do(func() {
		select {
		case <-s.done:
			// no-op spinner (not a terminal): nothing to clear
			return
		default:
		}
		close(s.stopCh)
		<-s.done
		// Clear the line so the real output starts clean.
		fmt.Fprint(os.Stderr, "\r\033[K")
	})
}

func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
