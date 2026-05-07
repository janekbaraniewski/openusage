package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// Server receives RemoteEnvelope pushes from worker machines over TCP HTTP.
type Server struct {
	addr  string
	store *Store
}

func NewServer(addr string, store *Store) *Server {
	return &Server{addr: addr, store: store}
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

	srv := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		return srv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
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
	writeJSON(w, http.StatusOK, s.store.Snapshots())
}

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
