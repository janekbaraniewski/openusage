package main

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildClaudeCodeDemoSnapshot(now time.Time) core.UsageSnapshot {
	return core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "claude-code",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			// ── Usage gauges ──────────────────────────────────────
			"usage_five_hour":        {Used: ptr(6.0), Unit: "%", Window: "rolling-5h"},
			"usage_seven_day":        {Used: ptr(79.0), Unit: "%", Window: "rolling-7d"},
			"usage_seven_day_sonnet": {Used: ptr(56.0), Unit: "%", Window: "rolling-7d"},
			"usage_seven_day_opus":   {Used: ptr(24.0), Unit: "%", Window: "rolling-7d"},
			"usage_seven_day_cowork": {Used: ptr(16.0), Unit: "%", Window: "rolling-7d"},

			// ── 5h billing block ──────────────────────────────────
			"5h_block_cost":              {Used: ptr(316.69), Unit: "USD", Window: "5h"},
			"5h_block_input":             {Used: ptr(4210000.0), Unit: "tokens", Window: "5h"},
			"5h_block_cache_read_tokens": {Used: ptr(1820000.0), Unit: "tokens", Window: "5h"},
			"5h_block_msgs":              {Used: ptr(840.0), Unit: "messages", Window: "5h"},
			"5h_block_output":            {Used: ptr(1370000.0), Unit: "tokens", Window: "5h"},

			// ── 7-day totals ──────────────────────────────────────
			"7d_api_cost":            {Used: ptr(127.92), Unit: "USD", Window: "7d"},
			"7d_input_tokens":        {Used: ptr(5700000.0), Unit: "tokens", Window: "7d"},
			"7d_cache_read_tokens":   {Used: ptr(2180000.0), Unit: "tokens", Window: "7d"},
			"7d_cache_create_tokens": {Used: ptr(540000.0), Unit: "tokens", Window: "7d"},
			"7d_reasoning_tokens":    {Used: ptr(382000.0), Unit: "tokens", Window: "7d"},
			"7d_messages":            {Used: ptr(2640.0), Unit: "messages", Window: "7d"},
			"7d_output_tokens":       {Used: ptr(3010000.0), Unit: "tokens", Window: "7d"},

			// ── Lifetime / burn ───────────────────────────────────
			"all_time_api_cost": {Used: ptr(321.25), Unit: "USD"},
			"burn_rate":         {Used: ptr(570.95), Unit: "USD/h"},
			"today_api_cost":    {Used: ptr(316.69), Unit: "USD"},

			// ── Today tokens ──────────────────────────────────────
			"today_input_tokens":  {Used: ptr(3940000.0), Unit: "tokens", Window: "today"},
			"today_output_tokens": {Used: ptr(1280000.0), Unit: "tokens", Window: "today"},

			// ── Activity ──────────────────────────────────────────
			"messages_today":   {Used: ptr(2640.0), Unit: "messages", Window: "today"},
			"sessions_today":   {Used: ptr(3600.0), Unit: "sessions", Window: "today"},
			"tool_calls_today": {Used: ptr(4200.0), Unit: "calls", Window: "today"},
			"7d_tool_calls":    {Used: ptr(5300.0), Unit: "calls", Window: "7d"},
			"today_cache_create_1h_tokens": {
				Used: ptr(142000.0), Unit: "tokens", Window: "today",
			},
			"today_cache_create_5m_tokens": {
				Used: ptr(48000.0), Unit: "tokens", Window: "today",
			},
			"today_web_search_requests": {Used: ptr(38.0), Unit: "requests", Window: "today"},
			"today_web_fetch_requests":  {Used: ptr(119.0), Unit: "requests", Window: "today"},
			"7d_web_search_requests":    {Used: ptr(182.0), Unit: "requests", Window: "7d"},
			"7d_web_fetch_requests":     {Used: ptr(467.0), Unit: "requests", Window: "7d"},

			// ── Model cost/token breakdown ────────────────────────
			"model_claude_opus_4_6_cost_usd":                {Used: ptr(516.56), Unit: "USD", Window: "7d"},
			"model_claude_opus_4_6_input_tokens":            {Used: ptr(5200000.0), Unit: "tokens", Window: "7d"},
			"model_claude_opus_4_6_output_tokens":           {Used: ptr(4200000.0), Unit: "tokens", Window: "7d"},
			"model_claude_haiku_4_5_20251001_cost_usd":      {Used: ptr(677.44), Unit: "USD", Window: "7d"},
			"model_claude_haiku_4_5_20251001_input_tokens":  {Used: ptr(6800000.0), Unit: "tokens", Window: "7d"},
			"model_claude_haiku_4_5_20251001_output_tokens": {Used: ptr(2600000.0), Unit: "tokens", Window: "7d"},
			"model_claude_sonnet_4_6_cost_usd":              {Used: ptr(8.12), Unit: "USD", Window: "7d"},
			"model_claude_sonnet_4_6_input_tokens":          {Used: ptr(320000.0), Unit: "tokens", Window: "7d"},
			"model_claude_sonnet_4_6_output_tokens":         {Used: ptr(180000.0), Unit: "tokens", Window: "7d"},

			// ── Client breakdown ──────────────────────────────────
			"client_webapp_input_tokens":     {Used: ptr(42100000.0), Unit: "tokens", Window: "7d"},
			"client_webapp_output_tokens":    {Used: ptr(14200000.0), Unit: "tokens", Window: "7d"},
			"client_webapp_cached_tokens":    {Used: ptr(38400000.0), Unit: "tokens", Window: "7d"},
			"client_webapp_reasoning_tokens": {Used: ptr(4100000.0), Unit: "tokens", Window: "7d"},
			"client_webapp_total_tokens":     {Used: ptr(98800000.0), Unit: "tokens", Window: "7d"},
			"client_webapp_sessions":         {Used: ptr(48.0), Unit: "sessions", Window: "7d"},
			"client_webapp_requests":         {Used: ptr(1100.0), Unit: "requests", Window: "7d"},

			"client_api_server_input_tokens":  {Used: ptr(38200000.0), Unit: "tokens", Window: "7d"},
			"client_api_server_output_tokens": {Used: ptr(12800000.0), Unit: "tokens", Window: "7d"},
			"client_api_server_total_tokens":  {Used: ptr(84700000.0), Unit: "tokens", Window: "7d"},
			"client_api_server_sessions":      {Used: ptr(36.0), Unit: "sessions", Window: "7d"},
			"client_api_server_requests":      {Used: ptr(942.0), Unit: "requests", Window: "7d"},

			"client_data_pipeline_total_tokens": {Used: ptr(25100000.0), Unit: "tokens", Window: "7d"},
			"client_data_pipeline_sessions":     {Used: ptr(14.0), Unit: "sessions", Window: "7d"},
			"client_data_pipeline_requests":     {Used: ptr(223.0), Unit: "requests", Window: "7d"},

			"client_infra_config_total_tokens": {Used: ptr(25000000.0), Unit: "tokens", Window: "7d"},
			"client_infra_config_sessions":     {Used: ptr(22.0), Unit: "sessions", Window: "7d"},
			"client_infra_config_requests":     {Used: ptr(447.0), Unit: "requests", Window: "7d"},

			"client_ci_cd_total_tokens": {Used: ptr(12400000.0), Unit: "tokens", Window: "7d"},
			"client_ci_cd_sessions":     {Used: ptr(8.0), Unit: "sessions", Window: "7d"},
			"client_ci_cd_requests":     {Used: ptr(189.0), Unit: "requests", Window: "7d"},

			"client_docs_site_total_tokens": {Used: ptr(9800000.0), Unit: "tokens", Window: "7d"},
			"client_docs_site_sessions":     {Used: ptr(6.0), Unit: "sessions", Window: "7d"},
			"client_docs_site_requests":     {Used: ptr(134.0), Unit: "requests", Window: "7d"},

			"client_test_suite_total_tokens": {Used: ptr(6200000.0), Unit: "tokens", Window: "7d"},
			"client_test_suite_sessions":     {Used: ptr(4.0), Unit: "sessions", Window: "7d"},
			"client_test_suite_requests":     {Used: ptr(87.0), Unit: "requests", Window: "7d"},

			// ── Tool usage (real provider uses tool_<name> format) ─
			"tool_bash":         {Used: ptr(1180.0), Unit: "calls", Window: "all-time estimate"},
			"tool_read":         {Used: ptr(737.0), Unit: "calls", Window: "all-time estimate"},
			"tool_bash_today":   {Used: ptr(618.0), Unit: "calls", Window: "all-time estimate"},
			"tool_read_today":   {Used: ptr(457.0), Unit: "calls", Window: "all-time estimate"},
			"tool_shell":        {Used: ptr(364.0), Unit: "calls", Window: "all-time estimate"},
			"tool_webfetch":     {Used: ptr(196.0), Unit: "calls", Window: "all-time estimate"},
			"tool_edit":         {Used: ptr(183.0), Unit: "calls", Window: "all-time estimate"},
			"tool_write":        {Used: ptr(142.0), Unit: "calls", Window: "all-time estimate"},
			"tool_glob":         {Used: ptr(134.0), Unit: "calls", Window: "all-time estimate"},
			"tool_grep":         {Used: ptr(128.0), Unit: "calls", Window: "all-time estimate"},
			"tool_websearch":    {Used: ptr(96.0), Unit: "calls", Window: "all-time estimate"},
			"tool_task":         {Used: ptr(89.0), Unit: "calls", Window: "all-time estimate"},
			"tool_notebookedit": {Used: ptr(34.0), Unit: "calls", Window: "all-time estimate"},
			"tool_todowrite":    {Used: ptr(28.0), Unit: "calls", Window: "all-time estimate"},

			// ── Language usage ─────────────────────────────────────
			"lang_go":         {Used: ptr(2920.0), Unit: "requests", Window: "all-time estimate"},
			"lang_typescript": {Used: ptr(557.0), Unit: "requests", Window: "all-time estimate"},
			"lang_python":     {Used: ptr(541.0), Unit: "requests", Window: "all-time estimate"},
			"lang_markdown":   {Used: ptr(530.0), Unit: "requests", Window: "all-time estimate"},
			"lang_terraform":  {Used: ptr(513.0), Unit: "requests", Window: "all-time estimate"},
			"lang_yaml":       {Used: ptr(113.0), Unit: "requests", Window: "all-time estimate"},
			"lang_json":       {Used: ptr(89.0), Unit: "requests", Window: "all-time estimate"},
			"lang_shell":      {Used: ptr(76.0), Unit: "requests", Window: "all-time estimate"},
			"lang_docker":     {Used: ptr(42.0), Unit: "requests", Window: "all-time estimate"},
			"lang_sql":        {Used: ptr(34.0), Unit: "requests", Window: "all-time estimate"},
			"lang_make":       {Used: ptr(28.0), Unit: "requests", Window: "all-time estimate"},
			"lang_rust":       {Used: ptr(21.0), Unit: "requests", Window: "all-time estimate"},

			// ── Code statistics ────────────────────────────────────
			"composer_lines_added":   {Used: ptr(48000.0), Unit: "lines", Window: "all-time estimate"},
			"composer_lines_removed": {Used: ptr(12400.0), Unit: "lines", Window: "all-time estimate"},
			"composer_files_changed": {Used: ptr(323.0), Unit: "files", Window: "all-time estimate"},
			"scored_commits":         {Used: ptr(9.0), Unit: "commits", Window: "all-time estimate"},
			"ai_code_percentage":     {Used: ptr(100.0), Remaining: ptr(0.0), Limit: ptr(100.0), Unit: "%", Window: "all-time estimate"},
			"total_prompts":          {Used: ptr(8900.0), Unit: "prompts", Window: "all-time estimate"},
		},
		Resets: map[string]time.Time{
			"billing_block":   now.Add(2*time.Hour + 25*time.Minute),
			"usage_five_hour": now.Add(2*time.Hour + 25*time.Minute),
			"usage_seven_day": now.Add(3*24*time.Hour + 11*time.Hour),
		},
		Attributes: map[string]string{
			"account_email": "demo.user@example.test",
			"plan_type":     "max_5",
			"auth_type":     "api_key",
		},
		Raw: map[string]string{
			"account_email":      "demo.user@example.test",
			"model_usage":        "claude-opus-4-6: 43%, claude-haiku-4-5-20251001: 57%, claude-sonnet-4-6: 1%",
			"model_usage_window": "7d",
			"model_count":        "3",
			"block_start":        now.Add(-2*time.Hour - 35*time.Minute).UTC().Format(time.RFC3339),
			"block_end":          now.Add(2*time.Hour + 25*time.Minute).UTC().Format(time.RFC3339),
			"cache_usage":        "read 2.2M · create 540k (1h 142k, 5m 48k)",
			"tool_usage":         "bash: 1.2k, read: 737, bash_today: 618, read_today: 457, shell: 364, webfetch: 196",
			"tool_count":         "14",
			"language_usage":     "go: 2.9k, typescript: 557, python: 541, markdown: 530, terraform: 513, yaml: 113",
			"client_usage":       "Webapp 41%, API Server 35%, Data Pipeline 10%, Infra Config 10%",
			"project_count":      "7",
		},
		ModelUsage: []core.ModelUsageRecord{
			{
				RawModelID:       "claude-opus-4-6",
				Canonical:        "claude-opus-4-6",
				CanonicalFamily:  "claude",
				CanonicalVariant: "opus",
				CostUSD:          ptr(516.56),
				InputTokens:      ptr(5200000.0),
				OutputTokens:     ptr(4200000.0),
				CachedTokens:     ptr(1820000.0),
				ReasoningTokens:  ptr(382000.0),
				Window:           "7d",
				Confidence:       1.0,
			},
			{
				RawModelID:       "claude-haiku-4-5-20251001",
				Canonical:        "claude-haiku-4-5-20251001",
				CanonicalFamily:  "claude",
				CanonicalVariant: "haiku",
				CostUSD:          ptr(677.44),
				InputTokens:      ptr(6800000.0),
				OutputTokens:     ptr(2600000.0),
				CachedTokens:     ptr(540000.0),
				Window:           "7d",
				Confidence:       1.0,
			},
			{
				RawModelID:       "claude-sonnet-4-6",
				Canonical:        "claude-sonnet-4-6",
				CanonicalFamily:  "claude",
				CanonicalVariant: "sonnet",
				CostUSD:          ptr(8.12),
				InputTokens:      ptr(320000.0),
				OutputTokens:     ptr(180000.0),
				Window:           "7d",
				Confidence:       1.0,
			},
		},
		DailySeries: map[string][]core.TimePoint{
			"cost":     demoSeries(now, 44, 61, 53, 72, 84, 89, 109),
			"requests": demoSeries(now, 288, 301, 336, 354, 382, 415, 441),

			// client trends
			"tokens_client_webapp":        demoSeries(now, 12300, 14700, 15900, 18100, 19400, 21800, 24600),
			"tokens_client_api_server":    demoSeries(now, 10200, 11400, 12600, 13200, 14100, 15200, 16500),
			"tokens_client_data_pipeline": demoSeries(now, 3100, 3500, 3800, 4100, 4400, 4700, 5100),
			"tokens_client_infra_config":  demoSeries(now, 3000, 3400, 3700, 4000, 4300, 4600, 5000),
			"tokens_client_ci_cd":         demoSeries(now, 1600, 1800, 1900, 2100, 2200, 2400, 2600),
			"tokens_client_docs_site":     demoSeries(now, 1200, 1400, 1500, 1600, 1700, 1800, 2000),
			"tokens_client_test_suite":    demoSeries(now, 800, 900, 1000, 1100, 1200, 1300, 1400),

			// model trends
			"usage_model_claude-opus-4-6":           demoSeries(now, 15, 17, 19, 20, 22, 24, 26),
			"usage_model_claude-haiku-4-5-20251001": demoSeries(now, 22, 25, 28, 31, 34, 38, 42),
			"usage_model_claude-sonnet-4-6":         demoSeries(now, 1, 1, 1, 2, 2, 2, 3),
		},
		Message: "~$316.69 today · $570.95/h",
	}
}
