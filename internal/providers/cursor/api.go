package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (p *Provider) callDashboardAPI(ctx context.Context, baseURL, token, method string, result interface{}) error {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", baseURL, method)
	return p.doPost(ctx, token, url, result)
}

func (p *Provider) callDashboardAPIWithBody(ctx context.Context, baseURL, token, method string, body []byte, result interface{}) error {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", baseURL, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("cursor: HTTP %d (body read failed: %w)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("cursor: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (p *Provider) callRESTAPI(ctx context.Context, baseURL, token, path string, result interface{}) error {
	url := baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("cursor: HTTP %d (body read failed: %w)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("cursor: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (p *Provider) doPost(ctx context.Context, token, url string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("cursor: HTTP %d (body read failed: %w)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("cursor: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
