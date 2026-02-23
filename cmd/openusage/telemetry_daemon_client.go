package main

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

type daemonReadModelAccount struct {
	AccountID  string `json:"account_id"`
	ProviderID string `json:"provider_id"`
}

type daemonReadModelRequest struct {
	Accounts      []daemonReadModelAccount `json:"accounts"`
	ProviderLinks map[string]string        `json:"provider_links"`
}

type daemonReadModelResponse struct {
	Snapshots map[string]core.UsageSnapshot `json:"snapshots"`
}

type daemonHookResponse struct {
	Source    string   `json:"source"`
	Enqueued  int      `json:"enqueued"`
	Processed int      `json:"processed"`
	Ingested  int      `json:"ingested"`
	Deduped   int      `json:"deduped"`
	Failed    int      `json:"failed"`
	Warnings  []string `json:"warnings,omitempty"`
}

type daemonHealthResponse struct {
	Status             string `json:"status"`
	DaemonVersion      string `json:"daemon_version,omitempty"`
	APIVersion         string `json:"api_version,omitempty"`
	IntegrationVersion string `json:"integration_version,omitempty"`
}

type telemetryDaemonClient struct {
	socketPath string
	http       *http.Client
}

func newTelemetryDaemonClient(socketPath string) *telemetryDaemonClient {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true,
		DisableKeepAlives:  true,
	}
	return &telemetryDaemonClient{
		socketPath: socketPath,
		http: &http.Client{
			Transport: transport,
			Timeout:   12 * time.Second,
		},
	}
}

func (c *telemetryDaemonClient) Health(ctx context.Context) error {
	_, err := c.HealthInfo(ctx)
	return err
}

func (c *telemetryDaemonClient) HealthInfo(ctx context.Context) (daemonHealthResponse, error) {
	if c == nil || strings.TrimSpace(c.socketPath) == "" {
		return daemonHealthResponse{}, fmt.Errorf("daemon client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/healthz", nil)
	if err != nil {
		return daemonHealthResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return daemonHealthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return daemonHealthResponse{}, fmt.Errorf("daemon health status: %s", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		return daemonHealthResponse{Status: "ok"}, nil
	}
	var out daemonHealthResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return daemonHealthResponse{}, fmt.Errorf("decode daemon health response: %w", err)
	}
	if strings.TrimSpace(out.Status) == "" {
		out.Status = "ok"
	}
	return out, nil
}

func (c *telemetryDaemonClient) ReadModel(
	ctx context.Context,
	request daemonReadModelRequest,
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

	var out daemonReadModelResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode daemon read-model response: %w", err)
	}
	if out.Snapshots == nil {
		out.Snapshots = map[string]core.UsageSnapshot{}
	}
	return out.Snapshots, nil
}

func (c *telemetryDaemonClient) IngestHook(
	ctx context.Context,
	source string,
	accountID string,
	payload []byte,
) (daemonHookResponse, error) {
	endpoint := "http://unix/v1/hook/" + url.PathEscape(strings.TrimSpace(source))
	if strings.TrimSpace(accountID) != "" {
		endpoint += "?account_id=" + url.QueryEscape(strings.TrimSpace(accountID))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return daemonHookResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return daemonHookResponse{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return daemonHookResponse{}, fmt.Errorf("daemon hook ingest failed: %s", strings.TrimSpace(string(body)))
	}

	var out daemonHookResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return daemonHookResponse{}, fmt.Errorf("decode daemon hook response: %w", err)
	}
	return out, nil
}
