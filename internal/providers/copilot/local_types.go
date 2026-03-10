package copilot

import "encoding/json"

type assistantMsgData struct {
	Content      string          `json:"content"`
	ReasoningTxt string          `json:"reasoningText"`
	ToolRequests json.RawMessage `json:"toolRequests"`
}

type quotaSnapshotEntry struct {
	EntitlementRequests int     `json:"entitlementRequests"`
	UsedRequests        int     `json:"usedRequests"`
	RemainingPercentage float64 `json:"remainingPercentage"`
	ResetDate           string  `json:"resetDate"`
}

type assistantUsageData struct {
	Model            string                        `json:"model"`
	InputTokens      float64                       `json:"inputTokens"`
	OutputTokens     float64                       `json:"outputTokens"`
	CacheReadTokens  float64                       `json:"cacheReadTokens"`
	CacheWriteTokens float64                       `json:"cacheWriteTokens"`
	Cost             float64                       `json:"cost"`
	Duration         int64                         `json:"duration"`
	QuotaSnapshots   map[string]quotaSnapshotEntry `json:"quotaSnapshots"`
}

type sessionShutdownData struct {
	ShutdownType         string                         `json:"shutdownType"`
	TotalPremiumRequests int                            `json:"totalPremiumRequests"`
	TotalAPIDurationMs   int64                          `json:"totalApiDurationMs"`
	SessionStartTime     string                         `json:"sessionStartTime"`
	CodeChanges          shutdownCodeChanges            `json:"codeChanges"`
	ModelMetrics         map[string]shutdownModelMetric `json:"modelMetrics"`
}

type shutdownCodeChanges struct {
	LinesAdded    int `json:"linesAdded"`
	LinesRemoved  int `json:"linesRemoved"`
	FilesModified int `json:"filesModified"`
}

type shutdownModelMetric struct {
	Requests struct {
		Count int     `json:"count"`
		Cost  float64 `json:"cost"`
	} `json:"requests"`
	Usage struct {
		InputTokens      float64 `json:"inputTokens"`
		OutputTokens     float64 `json:"outputTokens"`
		CacheReadTokens  float64 `json:"cacheReadTokens"`
		CacheWriteTokens float64 `json:"cacheWriteTokens"`
	} `json:"usage"`
}
