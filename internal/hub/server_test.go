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

func TestHandleSnapshots_Empty(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/snapshots", nil)
	w := httptest.NewRecorder()
	srv.handleSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var snaps map[string]core.UsageSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snaps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("expected empty map, got %d entries", len(snaps))
	}
}

func TestHandleSnapshots_WithData(t *testing.T) {
	srv, store := newTestServer(t)
	store.Ingest(core.RemoteEnvelope{
		Machine:   "laptop",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("anthropic", "acct1"), makeSnap("openai", "acct2")},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/snapshots", nil)
	w := httptest.NewRecorder()
	srv.handleSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var snaps map[string]core.UsageSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snaps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	for _, key := range []string{"laptop:acct1", "laptop:acct2"} {
		if _, ok := snaps[key]; !ok {
			t.Errorf("missing key %q in response", key)
		}
	}
}

func TestHandleSnapshots_WrongMethod(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/snapshots", nil)
	w := httptest.NewRecorder()
	srv.handleSnapshots(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
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

// --- Auth tests ---

func TestAuth_DisabledWhenNoToken(t *testing.T) {
	srv, _ := newTestServer(t)
	if srv.AuthEnabled() {
		t.Fatal("AuthEnabled() = true, want false for empty token")
	}

	env := core.RemoteEnvelope{Machine: "m1", Snapshots: []core.UsageSnapshot{makeSnap("openai", "a")}}
	body, _ := json.Marshal(env)
	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("push with no token and auth disabled: status = %d, want 200", w.Code)
	}
}

func TestAuth_PushRequiresToken(t *testing.T) {
	t.Setenv("OPENUSAGE_HUB_TOKEN", "")
	store := NewStore(time.Minute)
	srv := NewServerWithAuth(":0", store, "secret-token")
	if !srv.AuthEnabled() {
		t.Fatal("AuthEnabled() = false, want true")
	}

	env := core.RemoteEnvelope{Machine: "m1", Snapshots: []core.UsageSnapshot{makeSnap("openai", "a")}}
	body, _ := json.Marshal(env)

	// Missing header -> 401.
	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: status = %d, want 401", w.Code)
	}

	// Wrong token -> 401.
	req = httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong")
	w = httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", w.Code)
	}

	// Correct token -> 200.
	req = httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	w = httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("correct token: status = %d, want 200", w.Code)
	}
	if len(store.Snapshots()) != 1 {
		t.Errorf("expected 1 snapshot ingested after authorized push, got %d", len(store.Snapshots()))
	}
}

func TestAuth_SnapshotsRequiresToken(t *testing.T) {
	t.Setenv("OPENUSAGE_HUB_TOKEN", "")
	store := NewStore(time.Minute)
	srv := NewServerWithAuth(":0", store, "s3cret")
	store.Ingest(core.RemoteEnvelope{Machine: "m", Snapshots: []core.UsageSnapshot{makeSnap("openai", "a")}})

	req := httptest.NewRequest(http.MethodGet, "/v1/snapshots", nil)
	w := httptest.NewRecorder()
	srv.handleSnapshots(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing token on snapshots: status = %d, want 401", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("expected WWW-Authenticate header on 401")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/snapshots", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	w = httptest.NewRecorder()
	srv.handleSnapshots(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authorized snapshots: status = %d, want 200", w.Code)
	}
}

func TestAuth_HealthNeverRequiresToken(t *testing.T) {
	t.Setenv("OPENUSAGE_HUB_TOKEN", "")
	store := NewStore(time.Minute)
	srv := NewServerWithAuth(":0", store, "any-token")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz should be unauthenticated: status = %d, want 200", w.Code)
	}
}

func TestAuth_EnvFallback(t *testing.T) {
	t.Setenv("OPENUSAGE_HUB_TOKEN", "env-token")
	store := NewStore(time.Minute)
	// Explicit token is empty → env var picked up.
	srv := NewServerWithAuth(":0", store, "")
	if !srv.AuthEnabled() {
		t.Fatal("expected auth enabled via env fallback")
	}

	env := core.RemoteEnvelope{Machine: "m", Snapshots: []core.UsageSnapshot{makeSnap("openai", "a")}}
	body, _ := json.Marshal(env)

	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer env-token")
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("env-token auth: status = %d, want 200", w.Code)
	}
}

func TestPush_BodyTooLarge(t *testing.T) {
	srv, _ := newTestServer(t)
	// Craft a body >4 MiB with a tiny, valid-looking prefix. MaxBytesReader
	// will trip before Unmarshal, so we just need lots of bytes.
	big := make([]byte, (4<<20)+1024)
	for i := range big {
		big[i] = 'x'
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(big))
	// MaxBytesReader requires a real ResponseWriter to wire up its close behavior.
	w := httptest.NewRecorder()
	srv.handlePush(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: status = %d, want 413", w.Code)
	}
}
