package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	// maxPushBodyBytes caps /v1/push request bodies to guard against a
	// misbehaving or malicious worker exhausting hub memory. 4 MiB is
	// comfortably larger than any realistic snapshot batch.
	maxPushBodyBytes = 4 << 20

	// envAuthToken is the environment variable fallback for HubConfig.AuthToken.
	envAuthToken = "OPENUSAGE_HUB_TOKEN"
)

// Server receives RemoteEnvelope pushes from worker machines over TCP HTTP.
//
// When authToken is non-empty, /v1/push and /v1/snapshots require the header
// "Authorization: Bearer <token>". /healthz is always unauthenticated so that
// liveness probes work without secrets. When authToken is empty, all endpoints
// are open — suitable only for trusted LAN deployments.
type Server struct {
	addr      string
	store     *Store
	authToken string
}

func NewServer(addr string, store *Store) *Server {
	return NewServerWithAuth(addr, store, "")
}

// NewServerWithAuth creates a Server that requires Bearer token auth on
// mutating / data endpoints when authToken is non-empty. If authToken is
// empty and the OPENUSAGE_HUB_TOKEN env var is set, the env var value is
// used — this mirrors the exporter-side convention.
func NewServerWithAuth(addr string, store *Store, authToken string) *Server {
	token := strings.TrimSpace(authToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv(envAuthToken))
	}
	return &Server{addr: addr, store: store, authToken: token}
}

// AuthEnabled reports whether the server requires a Bearer token.
func (s *Server) AuthEnabled() bool {
	return s.authToken != ""
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/push", s.handlePush)
	mux.HandleFunc("/v1/snapshots", s.handleSnapshots)
	mux.HandleFunc("/healthz", s.handleHealth)

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("hub: listen %s: %w", s.addr, err)
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// checkAuth returns true if the request is authorized (or auth is disabled).
// When returning false, it has already written a 401 response.
func (s *Server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.authToken == "" {
		return true
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="openusage-hub"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if got != s.authToken {
		w.Header().Set("WWW-Authenticate", `Bearer realm="openusage-hub", error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid bearer token"})
		return false
	}
	return true
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAuth(w, r) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPushBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// MaxBytesReader returns a "http: request body too large" error when
		// the cap is exceeded; report 413 in that case, 400 otherwise.
		if strings.Contains(err.Error(), "request body too large") {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
		return
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty body"})
		return
	}
	var env core.RemoteEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(env.Machine) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "machine name required"})
		return
	}
	s.store.Ingest(env)
	writeJSON(w, http.StatusOK, pushResponse{OK: true})
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.store.Snapshots())
}

// handleHealth is always unauthenticated. It leaks only the list of machine
// names, which is considered non-sensitive enough to keep liveness probes
// simple in containerised deployments.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:   "ok",
		Machines: s.store.MachineNames(),
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
