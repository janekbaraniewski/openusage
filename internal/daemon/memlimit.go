package daemon

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// defaultDaemonMemoryLimit is the soft Go heap limit applied at daemon start.
// It makes the GC work harder as the heap approaches the limit instead of
// letting a large re-ingest or read-model rebuild grow RSS unchecked. It is a
// soft limit: the runtime may exceed it rather than fail. It sits well above
// the daemon's ~250 MiB live heap; a limit just above the live heap would
// keep the GC running almost constantly.
const defaultDaemonMemoryLimit int64 = 1 << 30 // 1 GiB

// daemonMemoryLimitEnv overrides the limit. Accepts a byte count or a number
// with a KiB/MiB/GiB (or K/M/G) suffix; "off" or "0" disables it.
const daemonMemoryLimitEnv = "OPENUSAGE_DAEMON_MEMORY_LIMIT"

// applyDaemonMemoryLimit sets the soft memory limit and returns the value
// applied (0 when none). GOMEMLIMIT, when set, is honoured by the runtime and
// left alone.
func applyDaemonMemoryLimit() int64 {
	if strings.TrimSpace(os.Getenv("GOMEMLIMIT")) != "" {
		return 0
	}
	limit := defaultDaemonMemoryLimit
	if raw := strings.TrimSpace(os.Getenv(daemonMemoryLimitEnv)); raw != "" {
		parsed, ok := parseMemoryLimit(raw)
		if ok {
			limit = parsed
		}
	}
	if limit <= 0 {
		return 0
	}
	debug.SetMemoryLimit(limit)
	return limit
}

func parseMemoryLimit(raw string) (int64, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "off" || s == "0" {
		return 0, true
	}
	mult := int64(1)
	for _, suf := range []struct {
		s string
		m int64
	}{{"gib", 1 << 30}, {"mib", 1 << 20}, {"kib", 1 << 10}, {"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}, {"g", 1 << 30}, {"m", 1 << 20}, {"k", 1 << 10}, {"b", 1}} {
		if strings.HasSuffix(s, suf.s) {
			s = strings.TrimSpace(strings.TrimSuffix(s, suf.s))
			mult = suf.m
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * mult, true
}
