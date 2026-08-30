package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultQuotaEndpoint = "https://daily-cloudcode-pa.googleapis.com/v1internal"
	quotaUserAgent       = "antigravity"
)

type quotaSummaryResponse struct {
	Groups      []quotaGroup `json:"groups"`
	Description string       `json:"description,omitempty"`
}

type quotaGroup struct {
	Buckets     []quotaBucket `json:"buckets"`
	DisplayName string        `json:"displayName,omitempty"`
	Description string        `json:"description,omitempty"`
}

type quotaBucket struct {
	BucketID          string   `json:"bucketId"`
	DisplayName       string   `json:"displayName,omitempty"`
	Window            string   `json:"window,omitempty"`
	ResetTime         string   `json:"resetTime,omitempty"`
	Description       string   `json:"description,omitempty"`
	RemainingFraction *float64 `json:"remainingFraction"`
}

func retrieveUserQuotaSummary(ctx context.Context, accessToken, baseURL string, client *http.Client) (quotaSummaryResponse, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultQuotaEndpoint
	}
	apiURL := strings.TrimRight(baseURL, "/") + ":retrieveUserQuotaSummary"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return quotaSummaryResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", quotaUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return quotaSummaryResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return quotaSummaryResponse{}, fmt.Errorf("retrieveUserQuotaSummary HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var decoded quotaSummaryResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return quotaSummaryResponse{}, fmt.Errorf("parse retrieveUserQuotaSummary: %w", err)
	}
	return decoded, nil
}

func quotaMapFromSummary(summary quotaSummaryResponse) map[string]statusLineQuota {
	out := make(map[string]statusLineQuota)
	for _, group := range summary.Groups {
		for _, bucket := range group.Buckets {
			id := strings.TrimSpace(bucket.BucketID)
			if id == "" {
				continue
			}
			out[id] = statusLineQuota{
				RemainingFraction: bucket.RemainingFraction,
				ResetTime:         strings.TrimSpace(bucket.ResetTime),
			}
		}
	}
	return out
}
