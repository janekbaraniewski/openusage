package daemon

import (
	"log"
	"strings"
	"time"
)

func (s *Service) infof(event, format string, args ...any) {
	if s == nil || !s.cfg.Verbose {
		return
	}
	if strings.TrimSpace(format) == "" {
		log.Printf("daemon level=info event=%s", event)
		return
	}
	log.Printf("daemon level=info event=%s "+format, append([]any{event}, args...)...)
}

func (s *Service) warnf(event, format string, args ...any) {
	if s == nil || !s.cfg.Verbose {
		return
	}
	if strings.TrimSpace(format) == "" {
		log.Printf("daemon level=warn event=%s", event)
		return
	}
	log.Printf("daemon level=warn event=%s "+format, append([]any{event}, args...)...)
}

func (s *Service) shouldLog(key string, interval time.Duration) bool {
	if s == nil {
		return false
	}
	return s.logThrottle.Allow(key, interval, time.Now())
}
