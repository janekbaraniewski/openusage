package webserve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSnapshotsIncludeProjectedViews(t *testing.T) {
	srv := testServer(t, Options{Demo: true})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var env Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if len(env.Views) == 0 {
		t.Fatal("expected projected views")
	}
	if env.ThemeTokens.Base == "" {
		t.Fatal("expected theme tokens")
	}
	if env.Views[0].Key == "" || len(env.Views[0].TileLines) == 0 {
		t.Fatalf("incomplete view: %+v", env.Views[0])
	}
}
