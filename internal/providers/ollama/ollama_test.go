package ollama

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetch_Success(t *testing.T) {
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"0.16.3"}`))
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cloud":{"disabled":false,"source":"config"}}`))
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","model":"gpt-oss:20b","size":1234},{"name":"qwen3-vl:235b-cloud","model":"qwen3-vl:235b-cloud","remote_model":"qwen3-vl:235b","remote_host":"https://ollama.com:443","size":393}]}`))
		case "/api/ps":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","model":"gpt-oss:20b","size":1234,"size_vram":1024,"context_length":32768}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer localServer.Close()

	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
			resp := map[string]any{
				"ID":    "acct-123",
				"Email": "user@example.com",
				"Name":  "user",
				"Plan":  "pro",
				"session_usage": map[string]any{
					"percent":          23.0,
					"reset_in_seconds": 7200.0,
				},
				"weekly_usage": map[string]any{
					"percent":          12.0,
					"reset_in_seconds": 86400.0,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b"},{"name":"qwen3-vl:235b"},{"name":"deepseek-v3.1:671b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer cloudServer.Close()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "db.sqlite")
	logDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	serverConfigPath := filepath.Join(tmpDir, "server.json")
	if err := os.WriteFile(serverConfigPath, []byte(`{"disable_ollama_cloud":false}`), 0o644); err != nil {
		t.Fatalf("write server config: %v", err)
	}

	if err := createTestDB(dbPath); err != nil {
		t.Fatalf("create test db: %v", err)
	}

	now := time.Now().In(time.Local)
	today := now.Format("2006/01/02")
	t0 := now.Add(-1 * time.Minute).Format("15:04:05")
	t1 := now.Format("15:04:05")
	logData := fmt.Sprintf(`[GIN] %s - %s | 200 | 1.2s | 127.0.0.1 | POST     "/api/chat"`+"\n", today, t0) +
		fmt.Sprintf(`[GIN] %s - %s | 200 | 850ms | 127.0.0.1 | POST     "/v1/chat/completions"`+"\n", today, t1)
	if err := os.WriteFile(filepath.Join(logDir, "server.log"), []byte(logData), 0o644); err != nil {
		t.Fatalf("write server log: %v", err)
	}

	os.Setenv("TEST_OLLAMA_KEY", "test-key")
	defer os.Unsetenv("TEST_OLLAMA_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-ollama",
		Provider:  "ollama",
		Auth:      "local",
		APIKeyEnv: "TEST_OLLAMA_KEY",
		BaseURL:   localServer.URL,
		ExtraData: map[string]string{
			"db_path":        dbPath,
			"logs_dir":       logDir,
			"server_config":  serverConfigPath,
			"cloud_base_url": cloudServer.URL,
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK", snap.Status)
	}

	if got := metricValue(snap, "models_total"); got != 2 {
		t.Errorf("models_total = %v, want 2", got)
	}
	if got := metricValue(snap, "loaded_models"); got != 1 {
		t.Errorf("loaded_models = %v, want 1", got)
	}
	if got := metricValue(snap, "cloud_catalog_models"); got != 3 {
		t.Errorf("cloud_catalog_models = %v, want 3", got)
	}
	if got := metricValue(snap, "requests_today"); got != 2 {
		t.Errorf("requests_today = %v, want 2", got)
	}
	if got := metricValue(snap, "messages_today"); got != 4 {
		t.Errorf("messages_today = %v, want 4", got)
	}
	if got := metricValue(snap, "source_local_requests"); got != 3 {
		t.Errorf("source_local_requests = %v, want 3", got)
	}
	if got := metricValue(snap, "source_cloud_requests"); got != 2 {
		t.Errorf("source_cloud_requests = %v, want 2", got)
	}
	if got := metricValue(snap, "source_local_requests_today"); got != 2 {
		t.Errorf("source_local_requests_today = %v, want 2", got)
	}
	if got := metricValue(snap, "source_cloud_requests_today"); got != 2 {
		t.Errorf("source_cloud_requests_today = %v, want 2", got)
	}
	if got := metricValue(snap, "tool_read_file"); got != 1 {
		t.Errorf("tool_read_file = %v, want 1", got)
	}
	if got := metricValue(snap, "tool_web_search"); got != 1 {
		t.Errorf("tool_web_search = %v, want 1", got)
	}
	if got := metricValue(snap, "model_gpt_oss_20b_input_tokens"); got != 2 {
		t.Errorf("model_gpt_oss_20b_input_tokens = %v, want 2", got)
	}
	if got := metricValue(snap, "model_gpt_oss_20b_output_tokens"); got != 1 {
		t.Errorf("model_gpt_oss_20b_output_tokens = %v, want 1", got)
	}
	if got := metricValue(snap, "model_qwen3_vl_235b_cloud_input_tokens"); got != 2 {
		t.Errorf("model_qwen3_vl_235b_cloud_input_tokens = %v, want 2", got)
	}
	if got := metricValue(snap, "client_local_total_tokens"); got != 3 {
		t.Errorf("client_local_total_tokens = %v, want 3", got)
	}
	if got := metricValue(snap, "client_cloud_total_tokens"); got != 3 {
		t.Errorf("client_cloud_total_tokens = %v, want 3", got)
	}
	if got := metricValue(snap, "tokens_today"); got != 6 {
		t.Errorf("tokens_today = %v, want 6", got)
	}

	if email := snap.Attributes["account_email"]; email != "user@example.com" {
		t.Errorf("account_email = %q, want user@example.com", email)
	}
	if plan := snap.Attributes["plan_name"]; plan != "pro" {
		t.Errorf("plan_name = %q, want pro", plan)
	}
	if _, ok := snap.Metrics["usage_five_hour"]; !ok {
		t.Fatal("expected usage_five_hour metric")
	}
	if _, ok := snap.Metrics["usage_one_day"]; !ok {
		t.Fatal("expected usage_one_day metric")
	}
	if m := snap.Metrics["usage_five_hour"]; m.Used == nil || *m.Used != 23 {
		t.Fatalf("usage_five_hour used = %v, want 23", m.Used)
	}
	if m := snap.Metrics["usage_one_day"]; m.Used == nil || *m.Used != 12 {
		t.Fatalf("usage_one_day used = %v, want 12", m.Used)
	}
	if got := metricValue(snap, "requests_5h"); got < 1 {
		t.Errorf("requests_5h = %v, want >= 1", got)
	}
	if got := metricValue(snap, "requests_1d"); got < 1 {
		t.Errorf("requests_1d = %v, want >= 1", got)
	}
	if _, ok := snap.Resets["usage_five_hour"]; !ok {
		t.Fatal("expected usage_five_hour reset")
	}
	if _, ok := snap.Resets["usage_one_day"]; !ok {
		t.Fatal("expected usage_one_day reset")
	}

	if len(snap.ModelUsage) == 0 {
		t.Fatal("expected ModelUsage records")
	}
	if len(snap.DailySeries["messages"]) == 0 {
		t.Fatal("expected messages DailySeries")
	}
	if len(snap.DailySeries["requests"]) == 0 {
		t.Fatal("expected requests DailySeries from logs")
	}
	if len(snap.DailySeries["usage_model_gpt_oss_20b"]) == 0 {
		t.Fatal("expected usage_model_gpt_oss_20b DailySeries")
	}
	if len(snap.DailySeries["usage_source_local"]) == 0 {
		t.Fatal("expected usage_source_local DailySeries")
	}
	if len(snap.DailySeries["analytics_tokens"]) == 0 {
		t.Fatal("expected analytics_tokens DailySeries")
	}
	if len(snap.DailySeries["tokens_client_local"]) == 0 {
		t.Fatal("expected tokens_client_local DailySeries")
	}
}

func TestFetch_AuthRequired_CloudOnlyWithoutKey(t *testing.T) {
	os.Unsetenv("TEST_OLLAMA_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-ollama-cloud",
		Provider:  "ollama",
		Auth:      "api_key",
		APIKeyEnv: "TEST_OLLAMA_MISSING",
		ExtraData: map[string]string{
			"cloud_base_url": "https://ollama.com",
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_RateLimited_CloudOnly(t *testing.T) {
	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer cloudServer.Close()

	os.Setenv("TEST_OLLAMA_KEY", "test-key")
	defer os.Unsetenv("TEST_OLLAMA_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-ollama-cloud",
		Provider:  "ollama",
		Auth:      "api_key",
		APIKeyEnv: "TEST_OLLAMA_KEY",
		ExtraData: map[string]string{
			"cloud_base_url": cloudServer.URL,
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Fatalf("Status = %v, want LIMITED", snap.Status)
	}
}

func TestFetch_NoSyntheticUsageWithoutCloudWindows(t *testing.T) {
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"0.16.3"}`))
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cloud":{"disabled":false,"source":"config"}}`))
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","model":"gpt-oss:20b","size":1234}]}`))
		case "/api/ps":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","model":"gpt-oss:20b","size":1234,"size_vram":1024,"context_length":32768}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer localServer.Close()

	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ID":    "acct-123",
				"Email": "user@example.com",
				"Name":  "user",
				"Plan":  "free",
			})
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer cloudServer.Close()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "db.sqlite")
	logDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	serverConfigPath := filepath.Join(tmpDir, "server.json")
	if err := os.WriteFile(serverConfigPath, []byte(`{"disable_ollama_cloud":false}`), 0o644); err != nil {
		t.Fatalf("write server config: %v", err)
	}
	if err := createTestDB(dbPath); err != nil {
		t.Fatalf("create test db: %v", err)
	}

	now := time.Now().In(time.Local)
	today := now.Format("2006/01/02")
	t0 := now.Add(-2 * time.Minute).Format("15:04:05")
	logData := fmt.Sprintf(`[GIN] %s - %s | 200 | 1.2s | 127.0.0.1 | POST     "/api/chat"`+"\n", today, t0)
	if err := os.WriteFile(filepath.Join(logDir, "server.log"), []byte(logData), 0o644); err != nil {
		t.Fatalf("write server log: %v", err)
	}

	os.Setenv("TEST_OLLAMA_KEY", "test-key")
	defer os.Unsetenv("TEST_OLLAMA_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-ollama",
		Provider:  "ollama",
		Auth:      "local",
		APIKeyEnv: "TEST_OLLAMA_KEY",
		BaseURL:   localServer.URL,
		ExtraData: map[string]string{
			"db_path":        dbPath,
			"logs_dir":       logDir,
			"server_config":  serverConfigPath,
			"cloud_base_url": cloudServer.URL,
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if _, ok := snap.Metrics["usage_five_hour"]; ok {
		t.Fatal("did not expect synthetic usage_five_hour metric")
	}
	if _, ok := snap.Metrics["usage_one_day"]; ok {
		t.Fatal("did not expect synthetic usage_one_day metric")
	}
	if _, ok := snap.Resets["usage_five_hour"]; ok {
		t.Fatal("did not expect synthetic usage_five_hour reset")
	}
	if _, ok := snap.Resets["usage_one_day"]; ok {
		t.Fatal("did not expect synthetic usage_one_day reset")
	}
	if got := metricValue(snap, "requests_5h"); got < 1 {
		t.Errorf("requests_5h = %v, want >= 1", got)
	}
	if got := metricValue(snap, "requests_1d"); got < 1 {
		t.Errorf("requests_1d = %v, want >= 1", got)
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
	selected_model TEXT NOT NULL DEFAULT ''
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
	created_at TIMESTAMP NOT NULL
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

	if _, err := db.Exec(`INSERT INTO settings (id, context_length, selected_model) VALUES (1, 32768, 'gpt-oss:20b')`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO chats (id, title, created_at) VALUES ('chat-1', 'today', ?), ('chat-2', 'yesterday', ?)`, today, yesterday); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO messages (chat_id, role, content, model_name, created_at) VALUES
		('chat-1','user','hello','gpt-oss:20b',?),
		('chat-1','assistant','hi','gpt-oss:20b',?),
		('chat-1','user','again','qwen3-vl:235b-cloud',?),
		('chat-1','assistant','done','qwen3-vl:235b-cloud',?),
		('chat-2','user','old','gpt-oss:20b',?)`,
		today, today, today, today, yesterday); err != nil {
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
