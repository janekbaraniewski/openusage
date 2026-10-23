package dsh

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestReadSessionFile_UsageAttributedByRequestContext(t *testing.T) {
	path := writeSession(t, t.TempDir(), "proj", "sess-1",
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		contextLine(1, fixtureDay1+10, "deepseek-official", "deepseek-flash"),
		`{"type":"user/message","seq":2,"time":1,"data":{"message":{"content":"ignored"}}}`,
		messageLine(3, fixtureDay1+1000, "m1", 0,
			`{"inputTokens":174,"outputTokens":248,"cacheReadTokens":6272,"cacheWriteTokens":0,"totalTokens":6694}`),
		contextLine(4, fixtureDay1+2000, "commandcode", "deepseek/deepseek-v4.1-flash"),
		messageLine(5, fixtureDay1+3000, "m2", 1,
			`{"inputTokens":100,"outputTokens":50,"cacheReadTokens":10,"reasoningTokens":20,"totalTokens":160}`),
	)

	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if session == nil {
		t.Fatal("session = nil")
	}
	if session.ID != "sess-1" || session.CWD != "/work/alpha" {
		t.Errorf("session = (%q, %q), want (sess-1, /work/alpha)", session.ID, session.CWD)
	}
	if !session.CreatedAt.Equal(time.UnixMilli(fixtureDay1).UTC()) {
		t.Errorf("CreatedAt = %v", session.CreatedAt)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(session.Messages))
	}

	first := session.Messages[0]
	if first.ID != "m1" || first.Model != "deepseek-flash" || first.Upstream != "deepseek" {
		t.Errorf("first = (%q, %q, %q), want (m1, deepseek-flash, deepseek)", first.ID, first.Model, first.Upstream)
	}
	if first.Input != 174 || first.Output != 248 || first.CacheRead != 6272 || first.CacheWrite != 0 || first.Total != 6694 {
		t.Errorf("first tokens = (%d,%d,%d,%d,%d), want (174,248,6272,0,6694)",
			first.Input, first.Output, first.CacheRead, first.CacheWrite, first.Total)
	}
	if !first.Time.Equal(time.UnixMilli(fixtureDay1 + 1000).UTC()) {
		t.Errorf("first.Time = %v", first.Time)
	}
	if first.Turn == nil || *first.Turn != 0 {
		t.Errorf("first.Turn = %v, want 0", first.Turn)
	}

	second := session.Messages[1]
	if second.Model != "deepseek/deepseek-v4.1-flash" || second.Upstream != "command_code" {
		t.Errorf("second = (%q, %q), want (deepseek/deepseek-v4.1-flash, command_code)", second.Model, second.Upstream)
	}
	if second.Reasoning != 20 || second.Total != 160 {
		t.Errorf("second reasoning/total = (%d,%d), want (20,160)", second.Reasoning, second.Total)
	}
}

func TestReadSessionFile_FallsBackToStreamResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		wantModel    string
		wantUpstream string
	}{
		{
			name:         "native deepseek client",
			response:     `{"kind":"deepseek-messages","version":1,"model":"deepseek-flash"}`,
			wantModel:    "deepseek-flash",
			wantUpstream: "deepseek",
		},
		{
			name:         "response names its provider",
			response:     `{"kind":"pi-ai","version":2,"api":"openai-completions","provider":"zai","model":"glm-5.3"}`,
			wantModel:    "glm-5.3",
			wantUpstream: "zai",
		},
		{
			name:         "no provider recorded",
			response:     `{"kind":"pi-ai","version":2,"model":"deepseek-like"}`,
			wantModel:    "deepseek-like",
			wantUpstream: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSession(t, t.TempDir(), "proj", "sess-1",
				headerLine("sess-1", fixtureDay1, "/work/alpha", false),
				messageLineWithResponse(1, fixtureDay1+1000, "m1", tt.response, usageJSON(10, 5, 0)),
			)
			session, err := readSessionFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(session.Messages) != 1 {
				t.Fatalf("len(Messages) = %d, want 1", len(session.Messages))
			}
			got := session.Messages[0]
			if got.Model != tt.wantModel || got.Upstream != tt.wantUpstream {
				t.Errorf("got (%q, %q), want (%q, %q)", got.Model, got.Upstream, tt.wantModel, tt.wantUpstream)
			}
		})
	}
}

func TestReadSessionFile_SkipsLinesWithoutUsage(t *testing.T) {
	path := writeSession(t, t.TempDir(), "proj", "sess-1",
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		contextLine(1, fixtureDay1, "zai", "glm-5.3"),
		`{"type":"assistant/message","seq":2,"time":1,"data":{"message":{"id":"no-usage"},"stream":[]}}`,
		`{"type":"assistant/message","seq":3,"time":1,"data":{"message":{"id":"null-usage"},"usage":null}}`,
		messageLine(4, fixtureDay1+1, "zero", 0, `{"inputTokens":0,"outputTokens":0,"totalTokens":0}`),
		`{"type":"assistant/message","seq":5,"time":1,"data":"not an object"}`,
		`this is not json`,
		``,
		// A line whose keys come in another order is still decoded.
		fmt.Sprintf(`{"seq":6,"time":%d,"data":{"message":{"id":"reordered"},"usage":{"inputTokens":7,"outputTokens":3,"totalTokens":10}},"type":"assistant/message"}`, fixtureDay1+2),
	)

	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1: %+v", len(session.Messages), session.Messages)
	}
	if got := session.Messages[0]; got.ID != "reordered" || got.Total != 10 || got.Model != "glm-5.3" {
		t.Errorf("got (%q, %d, %q), want (reordered, 10, glm-5.3)", got.ID, got.Total, got.Model)
	}
}

func TestReadSessionFile_CompactionSummary(t *testing.T) {
	path := writeSession(t, t.TempDir(), "proj", "sess-1",
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		contextLine(1, fixtureDay1, "deepseek-official", "deepseek-flash"),
		fmt.Sprintf(`{"type":"compaction/summary","seq":2,"time":%d,"data":{"compactionId":"c1","provider":"openai-codex","model":"gpt-6.1-sol","summary":"ignored","usage":{"inputTokens":900,"outputTokens":100,"cacheReadTokens":0,"totalTokens":1000}}}`, fixtureDay1+1),
	)

	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(session.Messages))
	}
	got := session.Messages[0]
	if got.ID != "compaction:c1" || got.Model != "gpt-6.1-sol" || got.Upstream != "codex" || got.Total != 1000 {
		t.Errorf("got (%q, %q, %q, %d), want (compaction:c1, gpt-6.1-sol, codex, 1000)", got.ID, got.Model, got.Upstream, got.Total)
	}
}

func TestReadSessionFile_ToolCalls(t *testing.T) {
	path := writeSession(t, t.TempDir(), "proj", "sess-1",
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		contextLine(1, fixtureDay1, "deepseek-official", "deepseek-flash"),
		toolCallLine(2, fixtureDay1+100, "call-ok", "read", `{"file_path":"/work/alpha/main.go","limit":20}`),
		toolCallLine(3, fixtureDay1+200, "call-err", "bash", `{"command":"false"}`),
		toolCallLine(4, fixtureDay1+300, "call-pending", "bash", `{"command":"sleep 60"}`),
		toolResultLine(5, fixtureDay1+400, "call-ok", false),
		toolResultLine(6, fixtureDay1+500, "call-err", true),
		toolResultLine(7, fixtureDay1+600, "call-unknown", false),
	)

	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(session.Tools) != 2 {
		t.Fatalf("len(Tools) = %d, want 2: %+v", len(session.Tools), session.Tools)
	}

	ok := session.Tools[0]
	if ok.CallID != "call-ok" || ok.Name != "read" || ok.IsError || ok.File != "/work/alpha/main.go" {
		t.Errorf("ok = %+v", ok)
	}
	if ok.Model != "deepseek-flash" || ok.Upstream != "deepseek" {
		t.Errorf("ok route = (%q, %q), want (deepseek-flash, deepseek)", ok.Model, ok.Upstream)
	}
	if !ok.Time.Equal(time.UnixMilli(fixtureDay1 + 100).UTC()) {
		t.Errorf("ok.Time = %v, want the call time", ok.Time)
	}

	failed := session.Tools[1]
	if failed.CallID != "call-err" || failed.Name != "bash" || !failed.IsError || failed.File != "" {
		t.Errorf("failed = %+v", failed)
	}
}

func TestReadSessionFile_SeededSessionSkipsInheritedHistory(t *testing.T) {
	// A seeded session opens with a copy of its source's log: same ids,
	// timestamps before this session was created.
	created := fixtureDay2
	path := writeSession(t, t.TempDir(), "proj", "fork-1",
		headerLine("fork-1", created, "/work/alpha", true),
		contextLine(1, fixtureDay1, "zai", "glm-5.3"),
		messageLine(2, fixtureDay1+1000, "inherited", 0, usageJSON(100, 50, 0)),
		toolCallLine(3, fixtureDay1+1100, "inherited-call", "bash", `{}`),
		toolResultLine(4, fixtureDay1+1200, "inherited-call", false),
		fmt.Sprintf(`{"type":"session/end-seed","seq":5,"time":%d,"data":{}}`, created),
		messageLine(6, created+1000, "own", 1, usageJSON(10, 5, 0)),
		toolCallLine(7, created+1100, "own-call", "bash", `{}`),
		toolResultLine(8, created+1200, "own-call", false),
	)

	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(session.Messages) != 1 || session.Messages[0].ID != "own" {
		t.Fatalf("Messages = %+v, want only \"own\"", session.Messages)
	}
	// The inherited request/context still names the route in effect.
	if got := session.Messages[0]; got.Model != "glm-5.3" || got.Upstream != "zai" {
		t.Errorf("route = (%q, %q), want (glm-5.3, zai)", got.Model, got.Upstream)
	}
	if len(session.Tools) != 1 || session.Tools[0].CallID != "own-call" {
		t.Fatalf("Tools = %+v, want only \"own-call\"", session.Tools)
	}
}

func TestReadSessionFile_ConcatenatedFrames(t *testing.T) {
	// dsh appends one Zstandard frame per flush.
	data := compressFrame(t,
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		contextLine(1, fixtureDay1, "zai", "glm-5.3"),
	)
	data = append(data, compressFrame(t, messageLine(2, fixtureDay1+1, "m1", 0, usageJSON(1, 2, 3)))...)
	data = append(data, compressFrame(t, messageLine(3, fixtureDay1+2, "m2", 0, usageJSON(4, 5, 6)))...)
	path := writeSessionBytes(t, t.TempDir(), "proj", "sess-1", data)

	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(session.Messages))
	}
}

func TestReadSessionFile_TruncatedLogKeepsCompleteEvents(t *testing.T) {
	data := compressFrame(t,
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		messageLine(1, fixtureDay1+1, "m1", 0, usageJSON(1, 2, 3)),
	)
	tail := compressFrame(t, messageLine(2, fixtureDay1+2, "m2", 0, usageJSON(4, 5, 6)))
	data = append(data, tail[:len(tail)/2]...)
	path := writeSessionBytes(t, t.TempDir(), "proj", "sess-1", data)

	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if session == nil || len(session.Messages) != 1 || session.Messages[0].ID != "m1" {
		t.Fatalf("session = %+v, want the one complete message", session)
	}
}

func TestReadSessionFile_NotASession(t *testing.T) {
	tests := []struct {
		name string
		data func(t *testing.T) []byte
	}{
		{name: "empty file", data: func(*testing.T) []byte { return nil }},
		{name: "not zstd", data: func(*testing.T) []byte { return []byte("plain text\n") }},
		{name: "no session header", data: func(t *testing.T) []byte {
			return compressFrame(t, messageLine(1, fixtureDay1, "m1", 0, usageJSON(1, 2, 3)))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSessionBytes(t, t.TempDir(), "proj", "sess-1", tt.data(t))
			session, err := readSessionFile(path)
			if session != nil {
				t.Errorf("session = %+v, want nil (err=%v)", session, err)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		session, err := readSessionFile(filepath.Join(t.TempDir(), sessionFileName))
		if session != nil || err != nil {
			t.Errorf("got (%+v, %v), want (nil, nil)", session, err)
		}
	})
}

func TestReadSessionFile_SessionIDFallsBackToDirectory(t *testing.T) {
	path := writeSession(t, t.TempDir(), "proj", "session-abc",
		fmt.Sprintf(`{"type":"session","version":4,"createdAt":%d,"cwd":"/work/alpha"}`, fixtureDay1),
		messageLine(1, fixtureDay1, "", 0, usageJSON(1, 2, 3)),
	)
	session, err := readSessionFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if session.ID != "session-abc" {
		t.Errorf("ID = %q, want session-abc", session.ID)
	}
	if len(session.Messages) != 1 || session.Messages[0].ID != "seq:1" {
		t.Errorf("Messages = %+v, want one message with id seq:1", session.Messages)
	}
}

func TestToolTargetFile(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		want      string
	}{
		{name: "encoded string", arguments: `"{\"file_path\":\"/a/b.go\",\"limit\":5}"`, want: "/a/b.go"},
		{name: "inline object", arguments: `{"file_path":"/a/c.ts"}`, want: "/a/c.ts"},
		{name: "no file argument", arguments: `"{\"command\":\"ls\"}"`, want: ""},
		{name: "non-string file argument", arguments: `{"file_path":42}`, want: ""},
		{name: "not json inside", arguments: `"ls -la"`, want: ""},
		{name: "null", arguments: `null`, want: ""},
		{name: "empty", arguments: ``, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolTargetFile([]byte(tt.arguments)); got != tt.want {
				t.Errorf("toolTargetFile(%s) = %q, want %q", tt.arguments, got, tt.want)
			}
		})
	}
}

func TestUpstreamProvider(t *testing.T) {
	tests := []struct {
		route        string
		responseKind string
		want         string
		wantTile     string
	}{
		{route: "deepseek-official", want: "deepseek", wantTile: "deepseek"},
		{route: " DeepSeek-Official ", want: "deepseek", wantTile: "deepseek"},
		{route: "commandcode", want: "command_code", wantTile: ID},
		{route: "openai-codex", want: "codex", wantTile: ID},
		{route: "zai", want: "zai", wantTile: ID},
		{route: "some-gateway", responseKind: "deepseek-messages", want: "some-gateway", wantTile: ID},
		{responseKind: "deepseek-messages", want: "deepseek", wantTile: "deepseek"},
		{responseKind: "pi-ai", want: "", wantTile: ID},
		{want: "", wantTile: ID},
	}
	for _, tt := range tests {
		t.Run(tt.route+"/"+tt.responseKind, func(t *testing.T) {
			got := upstreamProvider(tt.route, tt.responseKind)
			if got != tt.want {
				t.Errorf("upstreamProvider(%q, %q) = %q, want %q", tt.route, tt.responseKind, got, tt.want)
			}
			if tile := telemetryProviderID(got); tile != tt.wantTile {
				t.Errorf("telemetryProviderID(%q) = %q, want %q", got, tile, tt.wantTile)
			}
		})
	}
}
