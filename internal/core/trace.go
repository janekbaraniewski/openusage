package core

import (
	"log"
	"os"
	"sync"
)

var (
	traceEnabled     bool
	traceEnabledOnce sync.Once
)

func isTraceEnabled() bool {
	traceEnabledOnce.Do(func() {
		traceEnabled = os.Getenv("OPENUSAGE_DEBUG") != ""
	})
	return traceEnabled
}

// Tracef logs a formatted message to stderr when OPENUSAGE_DEBUG is set.
// The env check result is cached after the first call.
func Tracef(format string, args ...any) {
	if !isTraceEnabled() {
		return
	}
	log.Printf("[trace] "+format, args...)
}
