package dsh

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
	t.Setenv(homeEnvVar, "")
}

// Timestamps used across fixtures, as epoch milliseconds.
var (
	fixtureDay1 = time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC).UnixMilli()
	fixtureDay2 = time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC).UnixMilli()
)

// compressFrame returns lines as one Zstandard frame of JSONL.
func compressFrame(t *testing.T, lines ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := enc.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatalf("zstd write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	return buf.Bytes()
}

// writeSession writes a session log at <root>/<slug>/<id>/session.v4.jsonl.zstd.
func writeSession(t *testing.T, root, slug, id string, lines ...string) string {
	t.Helper()
	return writeSessionBytes(t, root, slug, id, compressFrame(t, lines...))
}

func writeSessionBytes(t *testing.T, root, slug, id string, data []byte) string {
	t.Helper()
	dir := filepath.Join(root, slug, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, sessionFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func headerLine(id string, createdAt int64, cwd string, seeded bool) string {
	return fmt.Sprintf(`{"type":"session","version":4,"id":%q,"createdAt":%d,"cwd":%q,"isSeeded":%t,"delegationDepth":0,"agentPreset":"standard"}`,
		id, createdAt, cwd, seeded)
}

func contextLine(seq, at int64, provider, model string) string {
	return fmt.Sprintf(`{"type":"request/context","seq":%d,"time":%d,"data":{"provider":%q,"model":%q,"contextWindow":128000}}`,
		seq, at, provider, model)
}

// messageLine is an assistant/message whose stream carries no response
// metadata, as dsh logs for some routes.
func messageLine(seq, at int64, id string, turn int, usage string) string {
	return fmt.Sprintf(`{"type":"assistant/message","seq":%d,"time":%d,"surfaceOp":"append","data":{"turn":%d,"step":0,"message":{"id":%q,"role":"assistant","content":[]},"stream":[{"chunk":null},{"chunk":{"type":"finish","reason":"stop"}}],"usage":%s}}`,
		seq, at, turn, id, usage)
}

// messageLineWithResponse is an assistant/message whose finish chunk names
// the response that produced it.
func messageLineWithResponse(seq, at int64, id, response, usage string) string {
	return fmt.Sprintf(`{"type":"assistant/message","seq":%d,"time":%d,"data":{"turn":0,"step":0,"message":{"id":%q},"stream":[{"chunk":null},{"chunk":{"type":"block-start"}},{"chunk":{"type":"finish","replayState":{"blocks":[],"response":%s}}}],"usage":%s}}`,
		seq, at, id, response, usage)
}

func toolCallLine(seq, at int64, callID, name, arguments string) string {
	return fmt.Sprintf(`{"type":"tool/call","seq":%d,"time":%d,"data":{"name":%q,"callId":%q,"arguments":%q,"turn":0,"step":0}}`,
		seq, at, name, callID, arguments)
}

func toolResultLine(seq, at int64, callID string, isError bool) string {
	return fmt.Sprintf(`{"type":"tool/result","seq":%d,"time":%d,"data":{"turn":0,"step":0,"message":{"id":"r-%s","role":"tool","toolCallId":%q,"isError":%t,"content":[]}}}`,
		seq, at, callID, callID, isError)
}

func usageJSON(input, output, cacheRead int64) string {
	return fmt.Sprintf(`{"inputTokens":%d,"outputTokens":%d,"cacheReadTokens":%d,"cacheWriteTokens":0,"totalTokens":%d}`,
		input, output, cacheRead, input+output+cacheRead)
}
