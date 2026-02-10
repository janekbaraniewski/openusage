package claude_code

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

func TestSanitizeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-opus-4-6", "claude_opus_4_6"},
		{"claude-opus-4-5-20251101", "claude_opus_4_5_20251101"},
		{"gpt-4.1-mini", "gpt_4_1_mini"},
		{"simple", "simple"},
	}
	for _, tt := range tests {
		got := sanitizeModelName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeModelName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestProvider_ID(t *testing.T) {
	p := New()
	if p.ID() != "claude_code" {
		t.Errorf("Expected ID 'claude_code', got %q", p.ID())
	}
}

func TestProvider_Describe(t *testing.T) {
	p := New()
	info := p.Describe()
	if info.Name != "Claude Code CLI" {
		t.Errorf("Expected name 'Claude Code CLI', got %q", info.Name)
	}
	if len(info.Capabilities) == 0 {
		t.Error("Expected non-empty capabilities")
	}
}

func TestProvider_Fetch_WithStatsFile(t *testing.T) {
	tmpDir := t.TempDir()
	statsPath := filepath.Join(tmpDir, "stats-cache.json")

	stats := statsCache{
		Version:          2,
		LastComputedDate: "2026-02-08",
		TotalSessions:    25,
		TotalMessages:    4264,
		DailyActivity: []dailyActivity{
			{Date: "2026-02-05", MessageCount: 100, SessionCount: 3, ToolCallCount: 20},
		},
		ModelUsage: map[string]modelUsage{
			"claude-opus-4-6": {
				InputTokens:  24389,
				OutputTokens: 75208,
			},
		},
	}

	data, _ := json.Marshal(stats)
	os.WriteFile(statsPath, data, 0644)

	accountPath := filepath.Join(tmpDir, ".claude.json")
	acctData := `{"hasAvailableSubscription": true, "oauthAccount": {"emailAddress": "test@example.com", "displayName": "Test"}}`
	os.WriteFile(accountPath, []byte(acctData), 0644)

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:      "test-claude",
		Binary:  statsPath,
		BaseURL: accountPath,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Expected StatusOK, got %v (message: %s)", snap.Status, snap.Message)
	}

	if snap.Raw["account_email"] != "test@example.com" {
		t.Errorf("Expected email 'test@example.com', got %q", snap.Raw["account_email"])
	}

	if snap.Raw["subscription"] != "active" {
		t.Errorf("Expected subscription 'active', got %q", snap.Raw["subscription"])
	}

	if m, ok := snap.Metrics["total_messages"]; ok {
		if m.Used == nil || *m.Used != 4264 {
			t.Errorf("Expected total_messages=4264, got %v", m.Used)
		}
	} else {
		t.Error("Expected total_messages metric")
	}
}

func TestProvider_Fetch_NoData(t *testing.T) {
	tmpDir := t.TempDir()
	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:      "test-claude",
		Binary:  filepath.Join(tmpDir, "nonexistent-stats.json"),
		BaseURL: filepath.Join(tmpDir, "nonexistent-account.json"),
		ExtraData: map[string]string{
			"claude_dir": filepath.Join(tmpDir, ".claude"),
		},
	})
	if err != nil {
		t.Fatalf("Fetch should not error, got: %v", err)
	}

	if snap.Status != core.StatusError {
		t.Errorf("Expected StatusError when no data, got %v", snap.Status)
	}
}

func TestEstimateCost_Opus(t *testing.T) {
	u := &jsonlUsage{
		InputTokens:              1000000, // 1M input
		OutputTokens:             100000,  // 100K output
		CacheReadInputTokens:     500000,  // 500K cache read
		CacheCreationInputTokens: 200000,  // 200K cache create
	}
	cost := estimateCost("claude-opus-4-6", u)
	expected := 27.0
	if math.Abs(cost-expected) > 0.01 {
		t.Errorf("estimateCost opus = %.4f, want %.4f", cost, expected)
	}
}

func TestEstimateCost_Sonnet(t *testing.T) {
	u := &jsonlUsage{
		InputTokens:  1000000,
		OutputTokens: 100000,
	}
	cost := estimateCost("claude-sonnet-4-5-20250929", u)
	expected := 4.5
	if math.Abs(cost-expected) > 0.01 {
		t.Errorf("estimateCost sonnet = %.4f, want %.4f", cost, expected)
	}
}

func TestEstimateCost_Haiku(t *testing.T) {
	u := &jsonlUsage{
		InputTokens:  1000000,
		OutputTokens: 1000000,
	}
	cost := estimateCost("claude-haiku-3-5-20241022", u)
	expected := 4.8
	if math.Abs(cost-expected) > 0.01 {
		t.Errorf("estimateCost haiku = %.4f, want %.4f", cost, expected)
	}
}

func TestFindPricing_Fallback(t *testing.T) {
	p := findPricing("claude-opus-9-9-20290101")
	if p.InputPerMillion != 15.0 {
		t.Errorf("Expected opus fallback pricing, got InputPerMillion=%f", p.InputPerMillion)
	}

	p = findPricing("claude-haiku-5-20290101")
	if p.InputPerMillion != 0.80 {
		t.Errorf("Expected haiku fallback pricing, got InputPerMillion=%f", p.InputPerMillion)
	}

	p = findPricing("totally-unknown-model")
	if p.InputPerMillion != 3.0 {
		t.Errorf("Expected sonnet fallback pricing, got InputPerMillion=%f", p.InputPerMillion)
	}
}

func TestParseJSONLFile(t *testing.T) {
	tmpDir := t.TempDir()
	fpath := filepath.Join(tmpDir, "session.jsonl")

	now := time.Now()

	lines := []string{
		fmt.Sprintf(`{"type":"human","sessionId":"abc123","timestamp":"%s","message":{"role":"user"}}`,
			now.Add(-10*time.Minute).Format(time.RFC3339)),
		fmt.Sprintf(`{"type":"assistant","sessionId":"abc123","timestamp":"%s","message":{"model":"claude-opus-4-6","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":200,"cache_read_input_tokens":300,"service_tier":"standard"}}}`,
			now.Add(-5*time.Minute).Format(time.RFC3339)),
		`{broken json`,
		fmt.Sprintf(`{"type":"assistant","sessionId":"abc123","timestamp":"%s","message":{"model":"claude-opus-4-6","role":"assistant","usage":{"input_tokens":50,"output_tokens":25,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
			now.Add(-1*time.Minute).Format(time.RFC3339)),
	}

	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	os.WriteFile(fpath, []byte(content), 0644)

	entries := parseJSONLFile(fpath)
	if len(entries) != 3 { // 2 valid + 1 user = 3, the broken line is skipped
		t.Errorf("Expected 3 parsed entries, got %d", len(entries))
	}

	assistantCount := 0
	for _, e := range entries {
		if e.Type == "assistant" && e.Message != nil && e.Message.Usage != nil {
			assistantCount++
		}
	}
	if assistantCount != 2 {
		t.Errorf("Expected 2 assistant entries with usage, got %d", assistantCount)
	}
}

func TestCollectJSONLFiles(t *testing.T) {
	tmpDir := t.TempDir()

	projectDir := filepath.Join(tmpDir, "projects", "test-project-abc")
	os.MkdirAll(projectDir, 0755)

	os.WriteFile(filepath.Join(projectDir, "session1.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectDir, "session2.jsonl"), []byte("{}"), 0644)

	os.WriteFile(filepath.Join(projectDir, "notes.txt"), []byte("hello"), 0644)

	files := collectJSONLFiles(filepath.Join(tmpDir, "projects"))
	if len(files) != 2 {
		t.Errorf("Expected 2 JSONL files, got %d", len(files))
	}
}

func TestCollectJSONLFiles_NonexistentDir(t *testing.T) {
	files := collectJSONLFiles("/nonexistent/path")
	if len(files) != 0 {
		t.Errorf("Expected 0 files for nonexistent dir, got %d", len(files))
	}
}

func TestProvider_Fetch_WithJSONL(t *testing.T) {
	tmpDir := t.TempDir()

	statsPath := filepath.Join(tmpDir, "stats-cache.json")
	stats := statsCache{Version: 2, TotalMessages: 100, TotalSessions: 5}
	data, _ := json.Marshal(stats)
	os.WriteFile(statsPath, data, 0644)

	accountPath := filepath.Join(tmpDir, ".claude.json")
	os.WriteFile(accountPath, []byte(`{"hasAvailableSubscription":true}`), 0644)

	projectDir := filepath.Join(tmpDir, "projects", "test-project-abc")
	os.MkdirAll(projectDir, 0755)

	now := time.Now()
	sessionFile := filepath.Join(projectDir, "session1.jsonl")

	var content string
	for i := 0; i < 5; i++ {
		ts := now.Add(time.Duration(-i*10) * time.Minute).Format(time.RFC3339)
		line := fmt.Sprintf(`{"type":"assistant","sessionId":"sess1","timestamp":"%s","message":{"model":"claude-opus-4-6","role":"assistant","usage":{"input_tokens":1000,"output_tokens":500,"cache_creation_input_tokens":200,"cache_read_input_tokens":3000}}}`, ts)
		content += line + "\n"
	}
	os.WriteFile(sessionFile, []byte(content), 0644)

	p := New()
	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  "test",
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Raw:        make(map[string]string),
		Resets:     make(map[string]time.Time),
	}

	err := p.readConversationJSONL(filepath.Join(tmpDir, "projects"), "/nonexistent", &snap)
	if err != nil {
		t.Fatalf("readConversationJSONL failed: %v", err)
	}

	if snap.Raw["jsonl_files_found"] != "1" {
		t.Errorf("Expected 1 JSONL file found, got %q", snap.Raw["jsonl_files_found"])
	}

	if snap.Raw["jsonl_total_entries"] != "5" {
		t.Errorf("Expected 5 total entries, got %q", snap.Raw["jsonl_total_entries"])
	}

	if m, ok := snap.Metrics["today_api_cost"]; ok {
		if m.Used == nil || *m.Used <= 0 {
			t.Errorf("Expected positive today_api_cost, got %v", m.Used)
		}
	} else {
		t.Error("Expected today_api_cost metric from JSONL data")
	}

	if m, ok := snap.Metrics["5h_block_cost"]; ok {
		if m.Used == nil || *m.Used <= 0 {
			t.Errorf("Expected positive 5h_block_cost, got %v", m.Used)
		}
	} else {
		t.Error("Expected 5h_block_cost metric")
	}

	if m, ok := snap.Metrics["5h_block_msgs"]; ok {
		if m.Used == nil || *m.Used != 5 {
			t.Errorf("Expected 5 block messages, got %v", m.Used)
		}
	} else {
		t.Error("Expected 5h_block_msgs metric")
	}
}

func TestReadSettings(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "settings.json")
	os.WriteFile(settingsPath, []byte(`{"model":"claude-opus-4-6","alwaysThinkingEnabled":true}`), 0644)

	p := New()
	snap := core.QuotaSnapshot{
		Metrics: make(map[string]core.Metric),
		Raw:     make(map[string]string),
	}

	err := p.readSettings(settingsPath, &snap)
	if err != nil {
		t.Fatalf("readSettings failed: %v", err)
	}

	if snap.Raw["active_model"] != "claude-opus-4-6" {
		t.Errorf("Expected active_model 'claude-opus-4-6', got %q", snap.Raw["active_model"])
	}
	if snap.Raw["always_thinking"] != "true" {
		t.Errorf("Expected always_thinking 'true', got %q", snap.Raw["always_thinking"])
	}
}

func TestReadAccount_FullDetails(t *testing.T) {
	tmpDir := t.TempDir()
	accountPath := filepath.Join(tmpDir, ".claude.json")

	acctJSON := `{
		"hasAvailableSubscription": true,
		"oauthAccount": {
			"emailAddress": "user@corp.com",
			"displayName": "Test User",
			"billingType": "stripe",
			"hasExtraUsageEnabled": true,
			"organizationUuid": "org-abc123"
		},
		"numStartups": 42,
		"installMethod": "npm"
	}`
	os.WriteFile(accountPath, []byte(acctJSON), 0644)

	p := New()
	snap := core.QuotaSnapshot{
		Metrics: make(map[string]core.Metric),
		Raw:     make(map[string]string),
	}

	err := p.readAccount(accountPath, &snap)
	if err != nil {
		t.Fatalf("readAccount failed: %v", err)
	}

	if snap.Raw["account_email"] != "user@corp.com" {
		t.Errorf("Expected email, got %q", snap.Raw["account_email"])
	}
	if snap.Raw["billing_type"] != "stripe" {
		t.Errorf("Expected billing_type 'stripe', got %q", snap.Raw["billing_type"])
	}
	if snap.Raw["extra_usage_enabled"] != "true" {
		t.Errorf("Expected extra_usage_enabled 'true', got %q", snap.Raw["extra_usage_enabled"])
	}
	if snap.Raw["subscription"] != "active" {
		t.Errorf("Expected subscription 'active', got %q", snap.Raw["subscription"])
	}
	if snap.Raw["num_startups"] != "42" {
		t.Errorf("Expected num_startups '42', got %q", snap.Raw["num_startups"])
	}
	if snap.Raw["install_method"] != "npm" {
		t.Errorf("Expected install_method 'npm', got %q", snap.Raw["install_method"])
	}
}

func TestFloorToHour(t *testing.T) {
	input := time.Date(2026, 2, 10, 14, 35, 22, 0, time.UTC)
	expected := time.Date(2026, 2, 10, 14, 0, 0, 0, time.UTC)
	got := floorToHour(input)
	if !got.Equal(expected) {
		t.Errorf("floorToHour(%v) = %v, want %v", input, got, expected)
	}
}
