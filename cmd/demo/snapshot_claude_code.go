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
			"model_claude_opus_4_6_cost_usd":                {Used: ptr(2162.4), Unit: "USD", Window: "30d"},
			"model_claude_opus_4_6_input_tokens":            {Used: ptr(829000000.0), Unit: "tokens", Window: "30d"},
			"model_claude_opus_4_6_output_tokens":           {Used: ptr(198000000.0), Unit: "tokens", Window: "30d"},
			"model_claude_haiku_4_5_20251001_cost_usd":      {Used: ptr(22.88), Unit: "USD", Window: "30d"},
			"model_claude_haiku_4_5_20251001_input_tokens":  {Used: ptr(146000000.0), Unit: "tokens", Window: "30d"},
			"model_claude_haiku_4_5_20251001_output_tokens": {Used: ptr(26200000.0), Unit: "tokens", Window: "30d"},
			"model_claude_sonnet_4_6_cost_usd":              {Used: ptr(3.92), Unit: "USD", Window: "30d"},
			"model_claude_sonnet_4_6_input_tokens":          {Used: ptr(4200000.0), Unit: "tokens", Window: "30d"},
			"model_claude_sonnet_4_6_output_tokens":         {Used: ptr(900000.0), Unit: "tokens", Window: "30d"},

			// ── Client breakdown ──────────────────────────────────
			"client_api_server_input_tokens":     {Used: ptr(196000000.0), Unit: "tokens", Window: "30d"},
			"client_api_server_output_tokens":    {Used: ptr(56400000.0), Unit: "tokens", Window: "30d"},
			"client_api_server_cached_tokens":    {Used: ptr(182000000.0), Unit: "tokens", Window: "30d"},
			"client_api_server_reasoning_tokens": {Used: ptr(28600000.0), Unit: "tokens", Window: "30d"},
			"client_api_server_total_tokens":     {Used: ptr(463000000.0), Unit: "tokens", Window: "30d"},
			"client_api_server_sessions":         {Used: ptr(41.0), Unit: "sessions", Window: "30d"},
			"client_api_server_requests":         {Used: ptr(5710.0), Unit: "requests", Window: "30d"},

			"client_docs_site_input_tokens":     {Used: ptr(124000000.0), Unit: "tokens", Window: "30d"},
			"client_docs_site_output_tokens":    {Used: ptr(36100000.0), Unit: "tokens", Window: "30d"},
			"client_docs_site_cached_tokens":    {Used: ptr(103000000.0), Unit: "tokens", Window: "30d"},
			"client_docs_site_reasoning_tokens": {Used: ptr(25900000.0), Unit: "tokens", Window: "30d"},
			"client_docs_site_total_tokens":     {Used: ptr(289000000.0), Unit: "tokens", Window: "30d"},
			"client_docs_site_sessions":         {Used: ptr(27.0), Unit: "sessions", Window: "30d"},
			"client_docs_site_requests":         {Used: ptr(2410.0), Unit: "requests", Window: "30d"},

			"client_cloud_agents_input_tokens":     {Used: ptr(108000000.0), Unit: "tokens", Window: "30d"},
			"client_cloud_agents_output_tokens":    {Used: ptr(33100000.0), Unit: "tokens", Window: "30d"},
			"client_cloud_agents_cached_tokens":    {Used: ptr(84200000.0), Unit: "tokens", Window: "30d"},
			"client_cloud_agents_reasoning_tokens": {Used: ptr(21700000.0), Unit: "tokens", Window: "30d"},
			"client_cloud_agents_total_tokens":     {Used: ptr(247000000.0), Unit: "tokens", Window: "30d"},
			"client_cloud_agents_sessions":         {Used: ptr(18.0), Unit: "sessions", Window: "30d"},
			"client_cloud_agents_requests":         {Used: ptr(2210.0), Unit: "requests", Window: "30d"},

			"client_ci_cd_input_tokens":     {Used: ptr(62000000.0), Unit: "tokens", Window: "30d"},
			"client_ci_cd_output_tokens":    {Used: ptr(18700000.0), Unit: "tokens", Window: "30d"},
			"client_ci_cd_cached_tokens":    {Used: ptr(41100000.0), Unit: "tokens", Window: "30d"},
			"client_ci_cd_reasoning_tokens": {Used: ptr(11900000.0), Unit: "tokens", Window: "30d"},
			"client_ci_cd_total_tokens":     {Used: ptr(156000000.0), Unit: "tokens", Window: "30d"},
			"client_ci_cd_sessions":         {Used: ptr(11.0), Unit: "sessions", Window: "30d"},
			"client_ci_cd_requests":         {Used: ptr(1320.0), Unit: "requests", Window: "30d"},

			// ── Project breakdown ─────────────────────────────────
			"project_platform_core_requests":             {Used: ptr(2658.0), Unit: "requests", Window: "30d"},
			"project_dashboard_shell_requests":           {Used: ptr(2162.0), Unit: "requests", Window: "30d"},
			"project_hardened_runtime_requests":          {Used: ptr(1132.0), Unit: "requests", Window: "30d"},
			"project_enterprise_ops_requests":            {Used: ptr(903.0), Unit: "requests", Window: "30d"},
			"project_cluster_runtime_requests":           {Used: ptr(824.0), Unit: "requests", Window: "30d"},
			"project_cluster_runtime_pro_requests":       {Used: ptr(479.0), Unit: "requests", Window: "30d"},
			"project_ui_shell_requests":                  {Used: ptr(196.0), Unit: "requests", Window: "30d"},
			"project_integration_lab_requests_today":     {Used: ptr(101.0), Unit: "requests", Window: "1d"},
			"project_dashboard_shell_requests_today":     {Used: ptr(21.0), Unit: "requests", Window: "1d"},
			"project_platform_core_requests_today":       {Used: ptr(0.0), Unit: "requests", Window: "1d"},
			"project_cluster_runtime_requests_today":     {Used: ptr(0.0), Unit: "requests", Window: "1d"},
			"project_cluster_runtime_pro_requests_today": {Used: ptr(0.0), Unit: "requests", Window: "1d"},

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

			// ── MCP servers ───────────────────────────────────────
			"mcp_calls_total":       {Used: ptr(1842.0), Unit: "calls", Window: "7d"},
			"mcp_calls_total_today": {Used: ptr(312.0), Unit: "calls", Window: "1d"},
			"mcp_servers_active":    {Used: ptr(5.0), Unit: "servers", Window: "7d"},

			"mcp_gopls_total":                {Used: ptr(624.0), Unit: "calls", Window: "7d"},
			"mcp_gopls_total_today":          {Used: ptr(98.0), Unit: "calls", Window: "1d"},
			"mcp_gopls_go_diagnostics":       {Used: ptr(218.0), Unit: "calls", Window: "7d"},
			"mcp_gopls_go_file_context":      {Used: ptr(186.0), Unit: "calls", Window: "7d"},
			"mcp_gopls_go_search":            {Used: ptr(124.0), Unit: "calls", Window: "7d"},
			"mcp_gopls_go_symbol_references": {Used: ptr(96.0), Unit: "calls", Window: "7d"},

			"mcp_github_total":               {Used: ptr(412.0), Unit: "calls", Window: "7d"},
			"mcp_github_total_today":         {Used: ptr(78.0), Unit: "calls", Window: "1d"},
			"mcp_github_create_pull_request": {Used: ptr(14.0), Unit: "calls", Window: "7d"},
			"mcp_github_get_pull_request":    {Used: ptr(86.0), Unit: "calls", Window: "7d"},
			"mcp_github_list_issues":         {Used: ptr(124.0), Unit: "calls", Window: "7d"},
			"mcp_github_search_code":         {Used: ptr(188.0), Unit: "calls", Window: "7d"},

			"mcp_linear_total":       {Used: ptr(348.0), Unit: "calls", Window: "7d"},
			"mcp_linear_total_today": {Used: ptr(64.0), Unit: "calls", Window: "1d"},
			"mcp_linear_list_issues": {Used: ptr(142.0), Unit: "calls", Window: "7d"},
			"mcp_linear_save_issue":  {Used: ptr(98.0), Unit: "calls", Window: "7d"},
			"mcp_linear_get_issue":   {Used: ptr(108.0), Unit: "calls", Window: "7d"},

			"mcp_slack_total":         {Used: ptr(286.0), Unit: "calls", Window: "7d"},
			"mcp_slack_total_today":   {Used: ptr(42.0), Unit: "calls", Window: "1d"},
			"mcp_slack_send_message":  {Used: ptr(94.0), Unit: "calls", Window: "7d"},
			"mcp_slack_read_channel":  {Used: ptr(112.0), Unit: "calls", Window: "7d"},
			"mcp_slack_search_public": {Used: ptr(80.0), Unit: "calls", Window: "7d"},

			"mcp_context7_total":              {Used: ptr(172.0), Unit: "calls", Window: "7d"},
			"mcp_context7_total_today":        {Used: ptr(30.0), Unit: "calls", Window: "1d"},
			"mcp_context7_resolve_library_id": {Used: ptr(86.0), Unit: "calls", Window: "7d"},
			"mcp_context7_query_docs":         {Used: ptr(86.0), Unit: "calls", Window: "7d"},

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
			"model_usage":        "claude-opus-4-6: 99%, claude-haiku-4-5-20251001: 1%, claude-sonnet-4-6: <1%",
			"model_usage_window": "30d",
			"model_count":        "3",
			"block_start":        now.Add(-2*time.Hour - 35*time.Minute).UTC().Format(time.RFC3339),
			"block_end":          now.Add(2*time.Hour + 25*time.Minute).UTC().Format(time.RFC3339),
			"cache_usage":        "read 2.2M · create 540k (1h 142k, 5m 48k)",
			"tool_usage":         "bash: 1.2k, read: 737, bash_today: 618, read_today: 457, shell: 364, webfetch: 196",
			"tool_count":         "14",
			"language_usage":     "go: 2.9k, typescript: 557, python: 541, markdown: 530, terraform: 513, yaml: 113",
			"client_usage":       "API Server 40%, Docs Site 24%, Cloud Agents 22%, CI Cd 14%",
			"project_usage":      "platform_core: 2.7k, dashboard_shell: 2.2k, hardened_runtime: 1.1k, enterprise_ops: 903, cluster_runtime: 824, cluster_runtime_pro: 479",
			"project_count":      "6",
		},
		ModelUsage: []core.ModelUsageRecord{
			{
				RawModelID:       "claude-opus-4-6",
				Canonical:        "claude-opus-4-6",
				CanonicalFamily:  "claude",
				CanonicalVariant: "opus",
				CostUSD:          ptr(2162.4),
				InputTokens:      ptr(829000000.0),
				OutputTokens:     ptr(198000000.0),
				CachedTokens:     ptr(112000000.0),
				ReasoningTokens:  ptr(11800000.0),
				Window:           "30d",
				Confidence:       1.0,
			},
			{
				RawModelID:       "claude-haiku-4-5-20251001",
				Canonical:        "claude-haiku-4-5-20251001",
				CanonicalFamily:  "claude",
				CanonicalVariant: "haiku",
				CostUSD:          ptr(22.88),
				InputTokens:      ptr(146000000.0),
				OutputTokens:     ptr(26200000.0),
				CachedTokens:     ptr(8700000.0),
				Window:           "30d",
				Confidence:       1.0,
			},
			{
				RawModelID:       "claude-sonnet-4-6",
				Canonical:        "claude-sonnet-4-6",
				CanonicalFamily:  "claude",
				CanonicalVariant: "sonnet",
				CostUSD:          ptr(3.92),
				InputTokens:      ptr(4200000.0),
				OutputTokens:     ptr(900000.0),
				Window:           "30d",
				Confidence:       1.0,
			},
		},
		DailySeries: map[string][]core.TimePoint{
			"analytics_cost":     demoPatternSeries(now, 392.15, demoPatternClaudeWindow...),
			"analytics_requests": demoPatternSeries(now, 1466, demoPatternClaudeWindow...),
			"analytics_tokens":   demoPatternSeries(now, 213891814, demoPatternClaudeWindow...),
			"cost":               demoPatternSeries(now, 392.15, demoPatternClaudeWindow...),
			"requests":           demoPatternSeries(now, 1466, demoPatternClaudeWindow...),
			"tokens_total":       demoPatternSeries(now, 213891814, demoPatternClaudeWindow...),

			// client trends
			"tokens_client_api_server":   demoPatternSeries(now, 27100, demoPatternClaudeWindow...),
			"tokens_client_docs_site":    demoPatternSeries(now, 16800, demoPatternClaudeSupport...),
			"tokens_client_cloud_agents": demoPatternSeries(now, 15100, demoPatternClaudeWindow...),
			"tokens_client_ci_cd":        demoPatternSeries(now, 6400, demoPatternClaudeSupport...),

			// model trends
			"usage_model_claude-opus-4-6":           demoPatternSeries(now, 1079, demoPatternClaudeWindow...),
			"usage_model_claude-haiku-4-5-20251001": demoPatternSeries(now, 454, demoPatternClaudeSupport...),
			"usage_model_synthetic":                 demoPatternSeries(now, 34, demoPatternClaudeSupport...),

			// project trends
			"usage_project_platform_core":       demoPatternSeries(now, 581, demoPatternClaudeWindow...),
			"usage_project_dashboard_shell":     demoPatternSeries(now, 974, demoPatternClaudeSupport...),
			"usage_project_hardened_runtime":    demoPatternSeries(now, 907, demoPatternClaudeLate...),
			"usage_project_enterprise_ops":      demoPatternSeries(now, 342, demoPatternClaudeSupport...),
			"usage_project_cluster_runtime":     demoPatternSeries(now, 276, demoPatternClaudeSupport...),
			"usage_project_cluster_runtime_pro": demoPatternSeries(now, 162, demoPatternClaudeLate...),
		},
		Message: "~$316.69 today · $570.95/h",
	}
}
