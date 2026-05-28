package codebuff

import (
	"strings"
	"time"
)

// parseChatIDTimestamp converts a Codebuff chatId of the form
// 2025-12-14T10-00-00.000Z back to a time.Time.
//
// The chatId is an ISO-8601 timestamp with the time-portion's `:` separators
// replaced by `-`. A naive global replace('-', ':') would corrupt the date
// portion (turning 2025-12-14 into 2025:12:14), so we split on the literal
// `T` and only rewrite the time half.
func parseChatIDTimestamp(chatID string) (time.Time, bool) {
	s := strings.TrimSpace(chatID)
	if s == "" {
		return time.Time{}, false
	}

	tIdx := strings.IndexByte(s, 'T')
	if tIdx < 0 {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t.UTC(), true
		}
		return time.Time{}, false
	}

	datePart := s[:tIdx]
	timePart := s[tIdx+1:]

	rebuilt := datePart + "T" + restoreColons(timePart)
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, rebuilt); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func restoreColons(timePart string) string {
	suffixStart := len(timePart)
	for i, r := range timePart {
		if r == 'Z' || r == '+' || (r == '-' && i >= 8) {
			suffixStart = i
			break
		}
	}
	head := timePart[:suffixStart]
	tail := timePart[suffixStart:]

	head = strings.Replace(head, "-", ":", 2)

	if len(tail) > 0 && tail[0] == '+' {
		tail = "+" + strings.Replace(tail[1:], "-", ":", 1)
	} else if len(tail) > 0 && tail[0] == '-' {
		tail = "-" + strings.Replace(tail[1:], "-", ":", 1)
	}

	return head + tail
}
