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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sunse666/NoneFrp/internal/client"
	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/link"
	"github.com/Sunse666/NoneFrp/internal/netinfo"
)

// Source provides the data rendered by the interface.
type Source interface {
	Status() client.Status
}

// Settings lets the interface read and change the agent configuration. When it
// is nil the settings section is read only.
type Settings interface {
	Config() config.Client
	Apply(cfg config.Client) error
	// Probe checks a credential without registering anything.
	Probe(ctx context.Context, relayURL, token string) (string, error)
}

// Server is the loopback web interface.
type Server struct {
	source   Source
	settings Settings
	token    string
	log      *slog.Logger
	tmpl     *template.Template
}

// New returns a web interface for source. When token is empty a random one is
// generated and used as the first path segment.
func New(source Source, settings Settings, token string, logger *slog.Logger) (*Server, error) {
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
	return &Server{source: source, settings: settings, token: token, log: logger, tmpl: tmpl}, nil
}

// Token returns the path segment that guards the interface.
func (s *Server) Token() string { return s.token }

// Handler returns the routes of the interface.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/"+s.token+"/", s.handleIndex)
	mux.HandleFunc("/"+s.token+"/api/status", s.handleStatus)
	mux.HandleFunc("/"+s.token+"/settings", s.handleSettings)
	mux.HandleFunc("/"+s.token+"/api/test", s.handleTest)
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

// pageData is what the template renders.
type pageData struct {
	Base       string
	CSRF       string
	Configured bool
	Relay      string
	Token      string
	LocalPort  int
	Direct     []string
	// DirectTailnet holds addresses on a mesh network such as Tailscale, which
	// only work for players who joined it.
	DirectTailnet []string
	RoomCode      string
	ProxyProtocol bool
	Editable      bool
	Message       string
	IsError       bool
}

func (s *Server) buildPage(r *http.Request) pageData {
	data := pageData{
		Base:     "/" + s.token,
		CSRF:     s.token,
		Editable: s.settings != nil,
		Message:  r.URL.Query().Get("msg"),
		IsError:  r.URL.Query().Get("err") == "1",
	}
	if s.settings == nil {
		return data
	}

	cfg := s.settings.Config()
	data.Configured = cfg.Validate() == nil

	urls := make([]string, 0, len(cfg.Relay))
	for _, relay := range cfg.Relay {
		urls = append(urls, relay.URL)
	}
	data.Relay = strings.Join(urls, "\n")
	data.Token = maskToken(cfg.Token)
	data.LocalPort = cfg.Local.Port
	data.RoomCode = cfg.Room.Code
	data.ProxyProtocol = cfg.Local.ProxyProtocol

	for _, candidate := range netinfo.Candidates() {
		described := netinfo.Describe(candidate, cfg.Local.Port)
		if candidate.Kind == netinfo.KindTailnet {
			data.DirectTailnet = append(data.DirectTailnet, described)
			continue
		}
		data.Direct = append(data.Direct, described)
	}
	return data
}

func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("•", len(token))
	}
	return token[:8] + "••••"
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, s.buildPage(r)); err != nil {
		s.log.Warn("rendering the web interface failed", "error", err)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(s.source.Status()); err != nil {
		s.log.Warn("rendering the status failed", "error", err)
	}
}

// handleSettings validates a submitted form, stores it and restarts the agent.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		http.Error(w, "settings are read only", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.redirect(w, r, "提交的表单无法解析", true)
		return
	}
	// The path token doubles as the CSRF token: another origin cannot know it.
	if r.PostFormValue("csrf") != s.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	cfg := s.settings.Config()

	// The form carries one address; the configuration can still hold several.
	relayURL := config.NormalizeRelayURL(r.PostFormValue("relay"))
	if relayURL != "" {
		cfg.Relay = []config.Relay{{Name: "relay-1", URL: relayURL}}
	}

	// An empty token field means "keep the current token".
	if token := strings.TrimSpace(r.PostFormValue("token")); token != "" {
		cfg.Token = token
	}

	port, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("local_port")))
	if err != nil || port < 1 || port > 65535 {
		s.redirect(w, r, "本地端口必须是 1 到 65535 之间的数字", true)
		return
	}
	cfg.Local.Port = port
	cfg.Local.ProxyProtocol = r.PostFormValue("proxy_protocol") != ""

	roomCode := strings.TrimSpace(r.PostFormValue("room_code"))
	if roomCode == "" {
		roomCode = "auto"
	}
	cfg.Room.Code = roomCode

	// A pasted join link carries everything, so it wins over the fields above.
	if raw := strings.TrimSpace(r.PostFormValue("join")); raw != "" {
		request, err := link.ParseJoin(raw)
		if err != nil {
			s.redirect(w, r, err.Error(), true)
			return
		}
		cfg.Relay = []config.Relay{{Name: "join-link", URL: request.RelayURL}}
		cfg.Token = request.Token
		if request.Room != "" {
			cfg.Room.Code = request.Room
		}
	}

	if err := s.settings.Apply(cfg); err != nil {
		s.log.Warn("applying settings failed", "error", err)
		s.redirect(w, r, err.Error(), true)
		return
	}
	s.redirect(w, r, "设置已保存，正在用新配置重新连接", false)
}

// handleTest answers the "test connection" button: it probes the credential
// without saving or spending it.
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if s.settings == nil {
		http.Error(w, "settings are read only", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeTestResult(w, false, "表单无法解析")
		return
	}
	if r.PostFormValue("csrf") != s.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	current := s.settings.Config()
	relayURL := config.NormalizeRelayURL(r.PostFormValue("relay"))
	if relayURL == "" && len(current.Relay) > 0 {
		relayURL = current.Relay[0].URL
	}
	token := strings.TrimSpace(r.PostFormValue("token"))
	if token == "" {
		token = current.Token
	}
	if relayURL == "" {
		writeTestResult(w, false, "还没有填写中继地址")
		return
	}
	if token == "" {
		writeTestResult(w, false, "还没有填写邀请码")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	domain, err := s.settings.Probe(ctx, relayURL, token)
	if err != nil {
		writeTestResult(w, false, err.Error())
		return
	}

	message := "连接成功"
	if domain != "" {
		message = "连接成功，房间地址会是 <房间码>." + domain
	}
	writeTestResult(w, true, message)
}

func writeTestResult(w http.ResponseWriter, ok bool, message string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "message": message})
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, message string, isError bool) {
	query := url.Values{}
	query.Set("msg", message)
	if isError {
		query.Set("err", "1")
	}
	http.Redirect(w, r, "/"+s.token+"/?"+query.Encode(), http.StatusSeeOther)
}
