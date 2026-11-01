package codex

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// Codex writes the live rollout as plain JSONL and newer CLI versions
// compress finished rollouts to .jsonl.zst. Both are session files.
const (
	sessionFileExt           = ".jsonl"
	compressedSessionFileExt = ".jsonl.zst"
)

func isCompressedSessionFile(path string) bool {
	return strings.HasSuffix(path, compressedSessionFileExt)
}

// collectSessionFileInfos lists plain and zstd-compressed session files under
// sessionsDir. When both foo.jsonl and foo.jsonl.zst exist (the CLI is
// compressing a finished rollout) only the plain file is kept, so a session
// is never counted twice.
func collectSessionFileInfos(sessionsDir string) (map[string]os.FileInfo, error) {
	infos, err := shared.CollectFilesWithStat([]string{sessionsDir}, map[string]bool{".jsonl": true, ".zst": true})
	if err != nil {
		return nil, err
	}
	for path := range infos {
		if strings.HasSuffix(path, sessionFileExt) {
			continue
		}
		if !isCompressedSessionFile(path) {
			delete(infos, path)
			continue
		}
		if _, plain := infos[strings.TrimSuffix(path, ".zst")]; plain {
			delete(infos, path)
		}
	}
	return infos, nil
}

// collectSessionFiles is collectSessionFileInfos as a sorted path list.
func collectSessionFiles(sessionsDir string) ([]string, error) {
	infos, err := collectSessionFileInfos(sessionsDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(infos))
	for path := range infos {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

// openSessionFile returns a reader over the decoded JSONL content of path,
// positioned at byteOffset of that content. Plain files seek; compressed
// files are decoded from the start and the prefix discarded.
func openSessionFile(path string, byteOffset int64) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !isCompressedSessionFile(path) {
		if _, err := f.Seek(byteOffset, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, err
		}
		return f, nil
	}
	dec, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1))
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("zstd reader: %w", err)
	}
	r := &zstdSessionReader{dec: dec, file: f}
	if byteOffset > 0 {
		if _, err := io.CopyN(io.Discard, dec, byteOffset); err != nil {
			_ = r.Close()
			return nil, err
		}
	}
	return r, nil
}

type zstdSessionReader struct {
	dec  *zstd.Decoder
	file *os.File
}

func (r *zstdSessionReader) Read(p []byte) (int, error) { return r.dec.Read(p) }

func (r *zstdSessionReader) Close() error {
	r.dec.Close()
	return r.file.Close()
}
