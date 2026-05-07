package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	store := NewStore(5 * time.Minute)
	srv := NewServer(":0", store)
	return srv, store
}

func TestHandlePush_Valid(t *testing.T) {
	srv, store := newTestServer(t)

	env := core.RemoteEnvelope{
		Machine:   "test-box",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "default")},
	}
	body, _ := json.Marshal(env)

	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePush(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	snaps := store.Snapshots()
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot ingested, got %d", len(snaps))
	}
	if _, ok := snaps["test-box:default"]; !ok {
		t.Error("expected key 'test-box:default'")
	}
}

func TestHandlePush_WrongMethod(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/push", nil)
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestHandlePush_EmptyBody(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader([]byte("   ")))
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandlePush_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader([]byte("{bad json")))
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandlePush_MissingMachine(t *testing.T) {
	srv, _ := newTestServer(t)
	env := core.RemoteEnvelope{Machine: "  ", Snapshots: nil}
	body, _ := json.Marshal(env)
	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	srv, store := newTestServer(t)
	store.Ingest(core.RemoteEnvelope{Machine: "box1", Snapshots: nil})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if len(resp.Machines) != 1 || resp.Machines[0] != "box1" {
		t.Errorf("machines = %v, want [box1]", resp.Machines)
	}
}
