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
	body, _ := io.ReadAll(resp.Body)
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

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("daemon read-model failed: %s", strings.TrimSpace(string(body)))
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

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return HookResponse{}, fmt.Errorf("daemon hook ingest failed: %s", strings.TrimSpace(string(body)))
	}

	var out HookResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return HookResponse{}, fmt.Errorf("decode daemon hook response: %w", err)
	}
	return out, nil
}
