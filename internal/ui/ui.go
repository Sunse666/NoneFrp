// Package ui serves the local web interface of the host agent.
//
// The interface is bound to loopback, lives under an unguessable path segment
// and checks the Host header, so a random web page cannot read the token or
// drive the agent.
package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Sunse666/NoneFrp/internal/client"
)

// Source provides the data rendered by the interface.
type Source interface {
	Status() client.Status
}

// Server is the loopback web interface.
type Server struct {
	source Source
	token  string
	log    *slog.Logger
	tmpl   *template.Template
}

// New returns a web interface for source. When token is empty a random one is
// generated and used as the first path segment.
func New(source Source, token string, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if token == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate ui token: %w", err)
		}
		token = hex.EncodeToString(buf)
	}
	tmpl, err := template.New("index").Parse(indexHTML)
	if err != nil {
		return nil, fmt.Errorf("parse ui template: %w", err)
	}
	return &Server{source: source, token: token, log: logger, tmpl: tmpl}, nil
}

// Token returns the path segment that guards the interface.
func (s *Server) Token() string { return s.token }

// Handler returns the routes of the interface.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/"+s.token+"/", s.handleIndex)
	mux.HandleFunc("/"+s.token+"/api/status", s.handleStatus)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	return s.guard(mux)
}

// guard rejects requests that did not come from this machine and this port.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			s.log.Warn("rejected a web request with a foreign host header", "host", r.Host)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}

// Start serves the interface until ctx is cancelled and returns its base URL.
func (s *Server) Start(ctx context.Context, addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen for the web interface: %w", err)
	}

	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Warn("web interface stopped", "error", err)
		}
	}()

	base := fmt.Sprintf("http://%s/%s/", ln.Addr().String(), s.token)
	s.log.Info("web interface ready", "url", base)
	return base, nil
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, map[string]string{
		"Base": "/" + s.token,
	}); err != nil {
		s.log.Warn("rendering the web interface failed", "error", err)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(s.source.Status()); err != nil {
		s.log.Warn("rendering the status failed", "error", err)
	}
}
