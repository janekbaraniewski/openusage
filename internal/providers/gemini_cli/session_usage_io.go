package gemini_cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
		if d == nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".json") {
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

func readGeminiChatFile(path string) (*geminiChatFile, error) {
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
