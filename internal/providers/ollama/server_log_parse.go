package ollama

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

func parseLogFile(path string, onEvent func(ginLogEvent)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	const maxLogLine = 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLogLine)

	for scanner.Scan() {
		line := scanner.Text()
		event, ok := parseGINLogLine(line)
		if !ok {
			continue
		}
		onEvent(event)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func parseGINLogLine(line string) (ginLogEvent, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[GIN]") {
		return ginLogEvent{}, false
	}

	parts := strings.Split(line, "|")
	if len(parts) < 5 {
		return ginLogEvent{}, false
	}

	left := strings.TrimSpace(strings.TrimPrefix(parts[0], "[GIN]"))
	leftParts := strings.Split(left, " - ")
	if len(leftParts) != 2 {
		return ginLogEvent{}, false
	}

	timestamp, err := time.ParseInLocation("2006/01/02 15:04:05", strings.TrimSpace(leftParts[0])+" "+strings.TrimSpace(leftParts[1]), time.Local)
	if err != nil {
		return ginLogEvent{}, false
	}

	status, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return ginLogEvent{}, false
	}

	durationText := strings.TrimSpace(parts[2])
	durationText = strings.ReplaceAll(durationText, "µ", "u")
	duration, err := time.ParseDuration(durationText)
	if err != nil {
		return ginLogEvent{}, false
	}

	methodPath := strings.TrimSpace(parts[4])
	methodPathParts := strings.Fields(methodPath)
	if len(methodPathParts) < 2 {
		return ginLogEvent{}, false
	}

	method := strings.TrimSpace(methodPathParts[0])
	path := strings.Trim(strings.TrimSpace(methodPathParts[1]), `"`)
	if method == "" || path == "" {
		return ginLogEvent{}, false
	}

	return ginLogEvent{
		Timestamp: timestamp,
		Status:    status,
		Duration:  duration,
		Method:    method,
		Path:      path,
	}, true
}

func isInferencePath(path string) bool {
	switch path {
	case "/api/chat", "/api/generate", "/api/embed", "/api/embeddings",
		"/v1/chat/completions", "/v1/completions", "/v1/responses", "/v1/embeddings", "/v1/messages":
		return true
	default:
		return false
	}
}
