package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/active"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type Client struct {
	SocketPath string
	http       *http.Client
}

func NewClient(socketPath string) *Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true,
		DisableKeepAlives:  true,
	}
	return &Client{
		SocketPath: socketPath,
		http: &http.Client{
			Transport: transport,
			Timeout:   12 * time.Second,
		},
	}
}

func (c *Client) HealthInfo(ctx context.Context) (HealthResponse, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return HealthResponse{}, fmt.Errorf("daemon client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/healthz", nil)
	if err != nil {
		return HealthResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return HealthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return HealthResponse{}, fmt.Errorf("daemon health status: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HealthResponse{}, fmt.Errorf("daemon: reading health response body: %w", err)
	}
	if len(body) == 0 {
		return HealthResponse{Status: "ok"}, nil
	}
	var out HealthResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return HealthResponse{}, fmt.Errorf("decode daemon health response: %w", err)
	}
	if strings.TrimSpace(out.Status) == "" {
		out.Status = "ok"
	}
	return out, nil
}

func (c *Client) ReadModel(
	ctx context.Context,
	request ReadModelRequest,
) (map[string]core.UsageSnapshot, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal daemon read-model request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://unix/v1/read-model",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon read-model failed: %s", strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("daemon: reading read-model response body: %w", err)
	}

	var out ReadModelResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode daemon read-model response: %w", err)
	}
	if out.Snapshots == nil {
		out.Snapshots = map[string]core.UsageSnapshot{}
	}
	return out.Snapshots, nil
}

// Active asks the daemon which configured provider is currently active.
func (c *Client) Active(ctx context.Context) (active.Selection, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return active.Selection{}, fmt.Errorf("daemon client is not configured")
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://unix/v1/active",
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return active.Selection{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return active.Selection{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return active.Selection{}, fmt.Errorf("daemon active failed: %s", strings.TrimSpace(string(body)))
	}
	var out active.Selection
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return active.Selection{}, fmt.Errorf("decode daemon active response: %w", err)
	}
	return out, nil
}

// ActiveList returns the daemon's current selector candidates for a switcher
// or other consumer that needs the evidence behind the active selection.
func (c *Client) ActiveList(ctx context.Context) (active.CandidateList, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return active.CandidateList{}, fmt.Errorf("daemon client is not configured")
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://unix/v1/active/list",
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return active.CandidateList{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return active.CandidateList{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return active.CandidateList{}, fmt.Errorf("daemon active list failed: %s", strings.TrimSpace(string(body)))
	}
	var out active.CandidateList
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return active.CandidateList{}, fmt.Errorf("decode daemon active list response: %w", err)
	}
	if out.Candidates == nil {
		out.Candidates = []active.Candidate{}
	}
	return out, nil
}

// ActiveDetail returns structured metric rows for the currently selected
// provider. It deliberately shares the daemon's active-selection decision.
func (c *Client) ActiveDetail(ctx context.Context) (active.DetailResponse, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return active.DetailResponse{}, fmt.Errorf("daemon client is not configured")
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://unix/v1/active/detail",
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return active.DetailResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return active.DetailResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return active.DetailResponse{}, fmt.Errorf("daemon active detail failed: %s", strings.TrimSpace(string(body)))
	}
	var out active.DetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return active.DetailResponse{}, fmt.Errorf("decode daemon active detail response: %w", err)
	}
	if out.Rows == nil {
		out.Rows = []active.DetailRow{}
	}
	return out, nil
}

// ActiveExplain asks the daemon to explain the exact selection decision it
// would make for the current telemetry/read-model state.
func (c *Client) ActiveExplain(ctx context.Context) (string, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return "", fmt.Errorf("daemon client is not configured")
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://unix/v1/active/explain",
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("daemon active explain failed: %s", strings.TrimSpace(string(body)))
	}
	var out ActiveExplainResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode daemon active explanation: %w", err)
	}
	return out.Explanation, nil
}

// SetPin stores a provider pin in the daemon. An empty key clears it.
func (c *Client) SetPin(ctx context.Context, key string) error {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return fmt.Errorf("daemon client is not configured")
	}
	payload, err := json.Marshal(ActivePinRequest{Key: strings.TrimSpace(key)})
	if err != nil {
		return fmt.Errorf("marshal active pin request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://unix/v1/active/pin",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon active pin failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) IngestHook(
	ctx context.Context,
	source string,
	accountID string,
	payload []byte,
) (HookResponse, error) {
	endpoint := "http://unix/v1/hook/" + url.PathEscape(strings.TrimSpace(source))
	if strings.TrimSpace(accountID) != "" {
		endpoint += "?account_id=" + url.QueryEscape(strings.TrimSpace(accountID))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return HookResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return HookResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return HookResponse{}, fmt.Errorf("daemon hook ingest failed: %s", strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HookResponse{}, fmt.Errorf("daemon: reading hook response body: %w", err)
	}

	var out HookResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return HookResponse{}, fmt.Errorf("decode daemon hook response: %w", err)
	}
	return out, nil
}
