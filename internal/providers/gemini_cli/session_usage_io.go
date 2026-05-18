package gemini_cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// findGeminiSessionFiles scans the Gemini CLI tmp dir for session transcripts,
// accommodating both the legacy and modern on-disk layouts:
//
//   - legacy:   `<tmpDir>/<uuid>/session-*.json`
//   - modern:   `<tmpDir>/<uuid>/chats/<name>.json` and `<name>.jsonl`
//
// Backup directories (anything under a `backup/` segment) are skipped; they
// shadow the active transcripts and would otherwise double-count usage.
func findGeminiSessionFiles(tmpDir string) ([]string, error) {
	if strings.TrimSpace(tmpDir) == "" {
		return nil, nil
	}
	if _, err := os.Stat(tmpDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat tmp dir: %w", err)
	}

	type item struct {
		path    string
		modTime time.Time
	}
	var files []item

	walkErr := filepath.WalkDir(tmpDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d == nil {
			return nil
		}
		// Reject backup subtrees outright.
		if d.IsDir() && strings.EqualFold(d.Name(), "backup") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if pathIsUnderBackup(path) {
			return nil
		}
		if !isGeminiSessionFile(path, d.Name()) {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		files = append(files, item{path: path, modTime: info.ModTime()})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk gemini tmp dir: %w", walkErr)
	}
	if len(files) == 0 {
		return nil, nil
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].path > files[j].path
		}
		return files[i].modTime.After(files[j].modTime)
	})

	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.path)
	}
	return paths, nil
}

// isGeminiSessionFile reports whether a directory entry should be parsed as a
// Gemini chat transcript. It accepts the legacy `session-*.json` pattern at
// any depth, plus `.json`/`.jsonl` files when they live under a `chats/`
// subdirectory of the tmp tree.
func isGeminiSessionFile(path, name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "session-") && strings.HasSuffix(lower, ".json") {
		return true
	}
	if !pathHasChatsSegment(path) {
		return false
	}
	return strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".jsonl")
}

func pathHasChatsSegment(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	// Only directory segments matter; the final component is the filename.
	if len(parts) < 2 {
		return false
	}
	for _, segment := range parts[:len(parts)-1] {
		if strings.EqualFold(segment, "chats") {
			return true
		}
	}
	return false
}

func pathIsUnderBackup(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, segment := range parts {
		if strings.EqualFold(segment, "backup") {
			return true
		}
	}
	return false
}

// readGeminiChatFile decodes a transcript at `path`, dispatching to the JSONL
// streaming reader when the file uses the line-delimited modern layout and to
// the structured JSON decoder otherwise.
func readGeminiChatFile(path string) (*geminiChatFile, error) {
	if strings.HasSuffix(strings.ToLower(path), ".jsonl") {
		return readGeminiChatJSONL(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chat geminiChatFile
	if err := json.NewDecoder(f).Decode(&chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

// readGeminiChatJSONL parses the modern, line-delimited Gemini chat format.
// Each line is one of:
//
//   - a chat header `{"sessionId":..., "startTime":..., ...}` (no `type`)
//   - a message `{"type":"user|model|...", "timestamp":..., "tokens":...}`
//
// Messages sharing an `id` collapse to the latest occurrence so streaming
// retransmits do not inflate totals. The file-level metadata is taken from
// the first header line we encounter.
func readGeminiChatJSONL(path string) (*geminiChatFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	chat := &geminiChatFile{}
	type sniff struct {
		Type      string `json:"type,omitempty"`
		SessionID string `json:"sessionId,omitempty"`
	}

	indexByID := make(map[string]int)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe sniff
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		// Header line: no message type; carries the session-level fields.
		if probe.Type == "" && probe.SessionID != "" {
			var header geminiChatFile
			if err := json.Unmarshal(line, &header); err == nil {
				if chat.SessionID == "" {
					chat.SessionID = header.SessionID
					chat.StartTime = header.StartTime
					chat.LastUpdated = header.LastUpdated
					chat.ProjectHash = header.ProjectHash
				}
			}
			continue
		}
		if probe.Type == "" {
			continue
		}
		var msg geminiChatMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.ID != "" {
			if idx, ok := indexByID[msg.ID]; ok {
				chat.Messages[idx] = msg
				continue
			}
			indexByID[msg.ID] = len(chat.Messages)
		}
		chat.Messages = append(chat.Messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if chat.SessionID == "" {
		// Derive a stable id from the path so downstream session dedup still
		// treats each JSONL file as one session.
		chat.SessionID = path
	}
	return chat, nil
}
