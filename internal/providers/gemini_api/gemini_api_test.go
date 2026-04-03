package gemini_api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func newModelsResponse() modelsResponse {
	return modelsResponse{
		Models: []modelInfo{
			{
				Name:                       "models/gemini-2.5-flash",
				DisplayName:                "Gemini 2.5 Flash",
				SupportedGenerationMethods: []string{"generateContent", "countTokens"},
				InputTokenLimit:            1048576,
				OutputTokenLimit:           65536,
			},
			{
				Name:                       "models/gemini-2.0-flash",
				DisplayName:                "Gemini 2.0 Flash",
				SupportedGenerationMethods: []string{"generateContent", "countTokens"},
				InputTokenLimit:            1048576,
				OutputTokenLimit:           8192,
			},
			{
				Name:                       "models/gemini-1.5-pro",
				DisplayName:                "Gemini 1.5 Pro",
				SupportedGenerationMethods: []string{"generateContent"},
				InputTokenLimit:            2097152,
				OutputTokenLimit:           8192,
			},
			{
				Name:                       "models/text-embedding-004",
				DisplayName:                "Text Embedding 004",
				SupportedGenerationMethods: []string{"embedContent"},
				InputTokenLimit:            2048,
				OutputTokenLimit:           768,
			},
		},
	}
}

func TestFetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the API key is passed as query parameter.
		if r.URL.Query().Get("key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("x-ratelimit-limit", "60")
		w.Header().Set("x-ratelimit-remaining", "58")
		w.Header().Set("x-ratelimit-reset", "30")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(newModelsResponse())
	}))
	defer server.Close()

	os.Setenv("TEST_GEMINI_KEY", "test-key")
	defer os.Unsetenv("TEST_GEMINI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gemini",
		Provider:  "gemini_api",
		APIKeyEnv: "TEST_GEMINI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	// 3 of 4 models support generateContent.
	models, ok := snap.Metrics["available_models"]
	if !ok {
		t.Fatal("missing available_models metric")
	}
	if models.Used == nil || *models.Used != 3 {
		t.Errorf("available_models used = %v, want 3", models.Used)
	}
	if models.Unit != "models" {
		t.Errorf("available_models unit = %q, want %q", models.Unit, "models")
	}

	// Verify models_sample raw field.
	sample, ok := snap.Raw["models_sample"]
	if !ok {
		t.Fatal("missing models_sample in raw")
	}
	if sample == "" {
		t.Error("models_sample should not be empty")
	}

	// Verify total_models raw field.
	if v := snap.Raw["total_models"]; v != "3" {
		t.Errorf("total_models = %q, want %q", v, "3")
	}

	// Verify token limits from gemini-2.5-flash.
	inputLimit, ok := snap.Metrics["input_token_limit"]
	if !ok {
		t.Fatal("missing input_token_limit metric")
	}
	if inputLimit.Limit == nil || *inputLimit.Limit != 1048576 {
		t.Errorf("input_token_limit = %v, want 1048576", inputLimit.Limit)
	}

	outputLimit, ok := snap.Metrics["output_token_limit"]
	if !ok {
		t.Fatal("missing output_token_limit metric")
	}
	if outputLimit.Limit == nil || *outputLimit.Limit != 65536 {
		t.Errorf("output_token_limit = %v, want 65536", outputLimit.Limit)
	}

	// Verify rate limit headers were parsed.
	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 60 {
		t.Errorf("rpm limit = %v, want 60", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 58 {
		t.Errorf("rpm remaining = %v, want 58", rpm.Remaining)
	}
}

func TestFetch_AuthRequired(t *testing.T) {
	os.Unsetenv("TEST_GEMINI_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gemini",
		Provider:  "gemini_api",
		APIKeyEnv: "TEST_GEMINI_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_InvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"message": "API key not valid"}}`))
	}))
	defer server.Close()

	os.Setenv("TEST_GEMINI_KEY", "bad-key")
	defer os.Unsetenv("TEST_GEMINI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gemini",
		Provider:  "gemini_api",
		APIKeyEnv: "TEST_GEMINI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{
			"error": {
				"message": "Resource has been exhausted",
				"details": [
					{
						"metadata": {
							"retryDelay": "38s"
						}
					}
				]
			}
		}`))
	}))
	defer server.Close()

	os.Setenv("TEST_GEMINI_KEY", "test-key")
	defer os.Unsetenv("TEST_GEMINI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gemini",
		Provider:  "gemini_api",
		APIKeyEnv: "TEST_GEMINI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusLimited {
		t.Errorf("Status = %v, want LIMITED", snap.Status)
	}

	if v := snap.Raw["retry_delay"]; v != "38s" {
		t.Errorf("retry_delay = %q, want %q", v, "38s")
	}
}
