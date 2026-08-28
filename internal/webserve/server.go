package webserve

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server serves the local web dashboard and snapshot JSON API.
type Server struct {
	mu        sync.Mutex
	addr      string
	authToken string
	collector *collector
}

func NewServer(opts Options) (*Server, error) {
	addr := normalizeListenAddr(opts.ListenAddr)
	token := strings.TrimSpace(opts.AuthToken)
	if err := ValidateExposure(addr, token, opts.AllowPublic); err != nil {
		return nil, err
	}
	return &Server{
		addr:      addr,
		authToken: token,
		collector: newCollector(opts),
	}, nil
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *Server) setAddr(addr string) {
	s.mu.Lock()
	s.addr = addr
	s.mu.Unlock()
}

func (s *Server) AuthEnabled() bool {
	return s.authToken != ""
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/v1/snapshots", s.handleSnapshots)
	mux.HandleFunc("/api/v1/meta", s.handleMeta)

	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ui assets missing"})
		})
		return mux
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/", fileServer)
	return mux
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.Addr())
	if err != nil {
		return fmt.Errorf("serve: listen %s: %w", s.Addr(), err)
	}
	s.setAddr(ln.Addr().String())

	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
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

func (s *Server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.authToken == "" {
		return true
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="openusage-serve"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.authToken)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="openusage-serve", error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid bearer token"})
		return false
	}
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	source := string(s.collector.source)
	if s.collector.demo {
		source = "demo"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"source": source,
	})
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAuth(w, r) {
		return
	}
	env, err := s.collector.envelope()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAuth(w, r) {
		return
	}
	env, err := s.collector.envelope()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"openusage_version":        env.OpenUsageVersion,
		"source":                   env.Source,
		"time_window":              env.TimeWindow,
		"theme":                    env.Theme,
		"refresh_interval_seconds": env.RefreshIntervalSeconds,
		"catalog":                  env.Catalog,
		"auth_required":            s.AuthEnabled(),
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
