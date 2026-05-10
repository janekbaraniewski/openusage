package ollama

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetchModelDetails(t *testing.T) {
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"0.16.3"}`))
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cloud":{"disabled":false}}`))
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3:8b","model":"llama3:8b","size":5000},{"name":"deepseek-r1:14b","model":"deepseek-r1:14b","size":8000},{"name":"gemma:2b","model":"gemma:2b","size":1500}]}`))
		case "/api/show":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			name := body["name"]
			switch name {
			case "llama3:8b":
				_, _ = w.Write([]byte(`{"capabilities":["completion","tools"],"details":{"family":"llama","parameter_size":"8B","quantization_level":"Q4_K_M"},"model_info":{"llama.context_length":8192}}`))
			case "deepseek-r1:14b":
				_, _ = w.Write([]byte(`{"capabilities":["completion","tools","thinking","vision"],"details":{"family":"deepseek","parameter_size":"14B","quantization_level":"Q5_K_M"},"model_info":{"deepseek.context_length":65536}}`))
			case "gemma:2b":
				_, _ = w.Write([]byte(`{"capabilities":["completion"],"details":{"family":"gemma","parameter_size":"2B","quantization_level":"Q4_0"},"model_info":{"gemma.context_length":8192}}`))
			default:
				http.NotFound(w, r)
			}
		case "/api/ps":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer localServer.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "test-ollama-details",
		Provider: "ollama",
		Auth:     "local",
		BaseURL:  localServer.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	// 2 models with tools: llama3 + deepseek-r1
	if got := metricValue(snap, "models_with_tools"); got != 2 {
		t.Errorf("models_with_tools = %v, want 2", got)
	}
	// 1 model with vision: deepseek-r1
	if got := metricValue(snap, "models_with_vision"); got != 1 {
		t.Errorf("models_with_vision = %v, want 1", got)
	}
	// 1 model with thinking: deepseek-r1
	if got := metricValue(snap, "models_with_thinking"); got != 1 {
		t.Errorf("models_with_thinking = %v, want 1", got)
	}
	// Max context should be 65536 from deepseek-r1
	if got := metricValue(snap, "max_context_length"); got != 65536 {
		t.Errorf("max_context_length = %v, want 65536", got)
	}
	// Total parameters: 8B + 14B + 2B = 24B
	if got := metricValue(snap, "total_parameters"); got != 24e9 {
		t.Errorf("total_parameters = %v, want 24e9", got)
	}

	// Check capability attributes
	if v := snap.Attributes["model_llama3_8b_capability_tools"]; v != "true" {
		t.Errorf("llama3:8b should have capability_tools = true, got %q", v)
	}
	if v := snap.Attributes["model_deepseek_r1_14b_capability_vision"]; v != "true" {
		t.Errorf("deepseek-r1:14b should have capability_vision = true, got %q", v)
	}
	if v := snap.Attributes["model_deepseek_r1_14b_capability_thinking"]; v != "true" {
		t.Errorf("deepseek-r1:14b should have capability_thinking = true, got %q", v)
	}
	if v := snap.Attributes["model_deepseek_r1_14b_quantization"]; v != "Q5_K_M" {
		t.Errorf("deepseek-r1:14b quantization = %q, want Q5_K_M", v)
	}
}

func TestThinkingMetricsFromDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "db.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE settings (id INTEGER PRIMARY KEY, context_length INTEGER DEFAULT 4096, selected_model TEXT DEFAULT '');
		CREATE TABLE chats (id TEXT PRIMARY KEY, title TEXT, created_at TIMESTAMP);
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT DEFAULT '',
			model_name TEXT,
			created_at TIMESTAMP,
			thinking_time_start TIMESTAMP,
			thinking_time_end TIMESTAMP
		);
		CREATE TABLE tool_calls (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id INTEGER, type TEXT, function_name TEXT, function_arguments TEXT, function_result TEXT);
		CREATE TABLE attachments (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id INTEGER);
		CREATE TABLE users (name TEXT, email TEXT, plan TEXT, cached_at TIMESTAMP);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}

	now := time.Now()
	today := now.Format("2006-01-02 15:04:05")

	_, _ = db.Exec(`INSERT INTO chats (id, title, created_at) VALUES ('c1', 'test', ?)`, today)

	// 3 thinking turns: 5s, 3s, 10s
	ts := []struct {
		model string
		start string
		end   string
	}{
		{"deepseek-r1:14b", now.Add(-60 * time.Second).Format("2006-01-02T15:04:05Z"), now.Add(-55 * time.Second).Format("2006-01-02T15:04:05Z")},
		{"deepseek-r1:14b", now.Add(-40 * time.Second).Format("2006-01-02T15:04:05Z"), now.Add(-37 * time.Second).Format("2006-01-02T15:04:05Z")},
		{"qwen3:32b", now.Add(-20 * time.Second).Format("2006-01-02T15:04:05Z"), now.Add(-10 * time.Second).Format("2006-01-02T15:04:05Z")},
	}
	for _, turn := range ts {
		_, _ = db.Exec(`INSERT INTO messages (chat_id, role, content, model_name, created_at, thinking_time_start, thinking_time_end) VALUES ('c1', 'assistant', 'resp', ?, ?, ?, ?)`,
			turn.model, today, turn.start, turn.end)
	}
	// Non-thinking message (should be excluded)
	_, _ = db.Exec(`INSERT INTO messages (chat_id, role, content, model_name, created_at) VALUES ('c1', 'user', 'hello', 'deepseek-r1:14b', ?)`, today)

	db.Close()

	// Minimal local server with no-op show endpoint
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.16.3"}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{}`))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer localServer.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "test-thinking",
		Provider: "ollama",
		Auth:     "local",
		BaseURL:  localServer.URL,
		RuntimeHints: map[string]string{
			"db_path": dbPath,
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := metricValue(snap, "thinking_requests"); got != 3 {
		t.Errorf("thinking_requests = %v, want 3", got)
	}
	// Total should be ~18s (5+3+10), allow some floating point slack
	if got := metricValue(snap, "total_thinking_seconds"); got < 17 || got > 19 {
		t.Errorf("total_thinking_seconds = %v, want ~18", got)
	}
	// Avg should be ~6s (18/3)
	if got := metricValue(snap, "avg_thinking_seconds"); got < 5 || got > 7 {
		t.Errorf("avg_thinking_seconds = %v, want ~6", got)
	}
}

func TestExpandedSettings(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "db.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE settings (
			id INTEGER PRIMARY KEY,
			context_length INTEGER DEFAULT 4096,
			selected_model TEXT DEFAULT '',
			websearch_enabled INTEGER DEFAULT 1,
			turbo_enabled INTEGER DEFAULT 0,
			think_enabled INTEGER DEFAULT 1,
			airplane_mode INTEGER DEFAULT 0,
			device_id TEXT DEFAULT 'test-device-123'
		);
		CREATE TABLE chats (id TEXT PRIMARY KEY, title TEXT, created_at TIMESTAMP);
		CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, chat_id TEXT, role TEXT, content TEXT, model_name TEXT, created_at TIMESTAMP);
		CREATE TABLE tool_calls (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id INTEGER, type TEXT, function_name TEXT, function_arguments TEXT, function_result TEXT);
		CREATE TABLE attachments (id INTEGER PRIMARY KEY AUTOINCREMENT, message_id INTEGER);
		CREATE TABLE users (name TEXT, email TEXT, plan TEXT, cached_at TIMESTAMP);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}

	_, _ = db.Exec(`INSERT INTO settings (id, context_length, selected_model, websearch_enabled, turbo_enabled, think_enabled, airplane_mode, device_id) VALUES (1, 8192, 'llama3:8b', 1, 0, 1, 0, 'test-device-123')`)
	db.Close()

	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.16.3"}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{}`))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer localServer.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "test-settings",
		Provider: "ollama",
		Auth:     "local",
		BaseURL:  localServer.URL,
		RuntimeHints: map[string]string{
			"db_path": dbPath,
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if v := snap.Attributes["selected_model"]; v != "llama3:8b" {
		t.Errorf("selected_model = %q, want llama3:8b", v)
	}
	if v := snap.Attributes["websearch_enabled"]; v != "1" {
		t.Errorf("websearch_enabled = %q, want 1", v)
	}
	if v := snap.Attributes["think_enabled"]; v != "1" {
		t.Errorf("think_enabled = %q, want 1", v)
	}
	if v := snap.Attributes["device_id"]; v != "test-device-123" {
		t.Errorf("device_id = %q, want test-device-123", v)
	}
}

func TestParseParameterSize(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"7B", 7e9},
		{"70B", 70e9},
		{"235B", 235e9},
		{"500M", 500e6},
		{"", 0},
		{"invalid", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := parseParameterSize(tt.in)
			if got != tt.want {
				t.Errorf("parseParameterSize(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDetailWidget(t *testing.T) {
	p := New()
	dw := p.DetailWidget()
	if len(dw.Sections) != 8 {
		t.Fatalf("DetailWidget sections = %d, want 8", len(dw.Sections))
	}
	expectedSections := []string{"Usage", "Models", "Languages", "MCP Usage", "Spending", "Trends", "Tokens", "Activity"}
	for i, s := range dw.Sections {
		if s.Name != expectedSections[i] {
			t.Errorf("section[%d] = %q, want %q", i, s.Name, expectedSections[i])
		}
		if s.Order != i+1 {
			t.Errorf("section[%d] order = %d, want %d", i, s.Order, i+1)
		}
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "Qwen3:32B:latest", want: "qwen3:32b"},
		{in: "models/gpt-oss:20b", want: "gpt-oss:20b"},
		{in: "https://ollama.com/library/deepseek-r1:70b-cloud", want: "deepseek-r1:70b-cloud"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := normalizeModelName(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeModelName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func metricValue(snap core.UsageSnapshot, key string) float64 {
	m, ok := snap.Metrics[key]
	if !ok || m.Remaining == nil {
		return -1
	}
	return *m.Remaining
}

func createTestDB(path string) error {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	defer db.Close()

	schema := `
CREATE TABLE settings (
	id INTEGER PRIMARY KEY,
	context_length INTEGER NOT NULL DEFAULT 4096,
	selected_model TEXT NOT NULL DEFAULT '',
	websearch_enabled INTEGER DEFAULT 0,
	turbo_enabled INTEGER DEFAULT 0,
	think_enabled INTEGER DEFAULT 1,
	airplane_mode INTEGER DEFAULT 0
);
CREATE TABLE chats (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL
);
CREATE TABLE messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_id TEXT NOT NULL,
	role TEXT NOT NULL,
	content TEXT NOT NULL DEFAULT '',
	model_name TEXT,
	created_at TIMESTAMP NOT NULL,
	thinking_time_start TIMESTAMP,
	thinking_time_end TIMESTAMP
);
CREATE TABLE tool_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	type TEXT NOT NULL DEFAULT 'function',
	function_name TEXT NOT NULL DEFAULT '',
	function_arguments TEXT NOT NULL DEFAULT '{}',
	function_result TEXT
);
CREATE TABLE attachments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL
);
CREATE TABLE users (
	name TEXT NOT NULL DEFAULT '',
	email TEXT NOT NULL DEFAULT '',
	plan TEXT NOT NULL DEFAULT '',
	cached_at TIMESTAMP NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	now := time.Now().In(time.Local)
	today := now.Format("2006-01-02 15:04:05")
	yesterday := now.Add(-24 * time.Hour).Format("2006-01-02 15:04:05")

	if _, err := db.Exec(`INSERT INTO settings (id, context_length, selected_model, websearch_enabled, think_enabled) VALUES (1, 32768, 'gpt-oss:20b', 1, 1)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO chats (id, title, created_at) VALUES ('chat-1', 'today', ?), ('chat-2', 'yesterday', ?)`, today, yesterday); err != nil {
		return err
	}
	thinkStart := now.Add(-30 * time.Second).Format("2006-01-02T15:04:05Z")
	thinkEnd := now.Add(-25 * time.Second).Format("2006-01-02T15:04:05Z")
	thinkStart2 := now.Add(-20 * time.Second).Format("2006-01-02T15:04:05Z")
	thinkEnd2 := now.Add(-17 * time.Second).Format("2006-01-02T15:04:05Z")

	if _, err := db.Exec(`INSERT INTO messages (chat_id, role, content, model_name, created_at, thinking_time_start, thinking_time_end) VALUES
		('chat-1','user','hello','gpt-oss:20b',?,NULL,NULL),
		('chat-1','assistant','hi','gpt-oss:20b',?,?,?),
		('chat-1','user','again','qwen3-vl:235b-cloud',?,NULL,NULL),
		('chat-1','assistant','done','qwen3-vl:235b-cloud',?,?,?),
		('chat-2','user','old','gpt-oss:20b',?,NULL,NULL)`,
		today, today, thinkStart, thinkEnd, today, today, thinkStart2, thinkEnd2, yesterday); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO tool_calls (message_id, type, function_name, function_arguments, function_result) VALUES
		(2, 'function', 'read_file', '{}', '{}'),
		(4, 'function', 'web_search', '{}', '{}')`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO attachments (message_id) VALUES (1)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO users (name, email, plan, cached_at) VALUES ('cached-user', 'cached@example.com', 'free', ?)`, today); err != nil {
		return err
	}

	return nil
}
