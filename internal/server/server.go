// Package server implements the relay control plane: it authenticates host
// agents, assigns rooms and keeps their lifecycle up to date.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/protocol"
	"github.com/Sunse666/NoneFrp/internal/room"
)

const (
	// writeWait bounds a single control frame write.
	writeWait = 10 * time.Second
	// pongWait is how long a peer may stay silent before it is dropped.
	pongWait = 45 * time.Second
	// pingPeriod must stay well below pongWait.
	pingPeriod = 20 * time.Second
	// readLimit caps a single control message.
	readLimit = 1 << 20
	// reapPeriod is how often expired rooms are collected.
	reapPeriod = 5 * time.Second
)

// ErrServerClosed is returned when the HTTP server stops serving.
var ErrServerClosed = errors.New("relay server closed")

// Server is the relay control-plane server.
type Server struct {
	cfg            config.Server
	rooms          *room.Registry
	plane          *dataplane
	limiter        *ipLimiter
	usage          *usageStore
	log            *slog.Logger
	upgrader       websocket.Upgrader
	publicListener net.Listener
}

// New returns a relay server for cfg.
func New(cfg config.Server, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:     cfg,
		rooms:   room.NewRegistry(cfg.GracePeriod.Std()),
		plane:   newDataplane(),
		limiter: newIPLimiter(cfg.Limits.ConnRatePerIP),
		usage:   newUsageStore(""),
		log:     logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// Agents are not browsers, so there is no meaningful origin to
			// check. Access is gated by the bearer token instead.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// SetStateFile makes credential usage survive a restart. Call it before Run.
func (s *Server) SetStateFile(path string) {
	s.usage = newUsageStore(path)
}

// Handler returns the HTTP routes of the relay.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agent", s.handleAgent)
	mux.HandleFunc("GET /v1/data", s.handleData)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	return mux
}

// Run serves the control plane until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if err := s.listenPublic(); err != nil {
		return err
	}
	go s.servePublic(ctx)
	defer s.plane.closeAll()

	serveErr := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	go s.reapLoop(ctx)

	s.log.Info("relay listening",
		"addr", s.cfg.Listen,
		"player_addr", s.PlayerAddr().String(),
		"domain_suffix", s.cfg.DomainSuffix,
		"tokens", len(s.cfg.Tokens),
	)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		s.log.Info("relay stopped")
		return nil
	case err := <-serveErr:
		if err != nil {
			return err
		}
		return ErrServerClosed
	}
}

func (s *Server) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(reapPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.limiter.gc()
			if removed := s.rooms.Reap(); removed > 0 {
				s.log.Info("reaped expired rooms", "count", removed, "remaining", s.rooms.Len())
				s.plane.retainOnly(s.liveRooms())
			}
		}
	}
}

// liveRooms returns the set of room codes that still exist.
func (s *Server) liveRooms() map[string]bool {
	snapshot := s.rooms.Snapshot()
	alive := make(map[string]bool, len(snapshot))
	for _, tracked := range snapshot {
		alive[tracked.Code] = true
	}
	return alive
}

func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if !s.authorized(token) {
		s.log.Warn("rejected agent", "remote", r.RemoteAddr, "reason", "invalid token")
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("websocket upgrade failed", "remote", r.RemoteAddr, "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	conn.SetReadLimit(readLimit)
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	peer := &peer{conn: conn}
	log := s.log.With("remote", r.RemoteAddr)

	_, raw, err := conn.ReadMessage()
	if err != nil {
		log.Warn("agent did not send a register message", "error", err)
		return
	}
	env, err := protocol.Decode(raw)
	if err != nil {
		_ = peer.send(protocol.TypeError, protocol.ErrorBody{Code: protocol.ErrBadRequest, Message: err.Error()})
		return
	}
	if env.Type != protocol.TypeRegister {
		_ = peer.send(protocol.TypeError, protocol.ErrorBody{
			Code:    protocol.ErrBadRequest,
			Message: "expected register, got " + string(env.Type),
		})
		return
	}
	reg, err := protocol.As[protocol.Register](env)
	if err != nil {
		_ = peer.send(protocol.TypeError, protocol.ErrorBody{Code: protocol.ErrBadRequest, Message: err.Error()})
		return
	}

	// The credential may be an invite with a device cap or an expiry.
	credential := s.cfg.Tokens[token]
	if err := credential.Check(reg.DeviceID, s.usage.devices(token), time.Now()); err != nil {
		code := protocol.ErrUnauthorized
		switch {
		case errors.Is(err, config.ErrInviteExpired):
			code = protocol.ErrInviteExpired
		case errors.Is(err, config.ErrInviteExhausted):
			code = protocol.ErrInviteExhausted
		}
		log.Warn("credential rejected", "device", reg.DeviceID, "reason", err)
		_ = peer.send(protocol.TypeError, protocol.ErrorBody{Code: code, Message: err.Error()})
		return
	}

	created, err := s.rooms.Create(reg.DeviceID, reg.RoomCode, "", s.cfg.PublicPort)
	if err != nil {
		code := protocol.ErrBadRequest
		if errors.Is(err, room.ErrCodeTaken) {
			code = protocol.ErrRoomConflict
		}
		log.Warn("room creation failed", "device", reg.DeviceID, "error", err)
		_ = peer.send(protocol.TypeError, protocol.ErrorBody{Code: code, Message: err.Error()})
		return
	}

	publicHost := s.cfg.RoomHost(created.Code)
	s.rooms.SetPublicHost(created.Code, publicHost)

	if err := peer.send(protocol.TypeRegistered, protocol.Registered{
		RoomCode:   created.Code,
		PublicHost: publicHost,
		PublicPort: created.PublicPort,
	}); err != nil {
		log.Warn("failed to confirm registration", "room", created.Code, "error", err)
		return
	}

	if err := s.usage.remember(token, reg.DeviceID); err != nil {
		log.Warn("could not record credential usage", "error", err)
	}

	log = log.With("room", created.Code, "device", reg.DeviceID)
	log.Info("room registered", "public", publicHost, "port", created.PublicPort)

	s.plane.setControl(created.Code, peer)
	defer s.plane.dropControl(created.Code, peer)

	defer func() {
		s.rooms.MarkGrace(created.Code)
		log.Info("agent disconnected", "grace_period", s.cfg.GracePeriod.Std().String())
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go peer.pingLoop(ctx, log)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Warn("control connection lost", "error", err)
			}
			return
		}
		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			return
		}

		env, err := protocol.Decode(raw)
		if err != nil {
			_ = peer.send(protocol.TypeError, protocol.ErrorBody{Code: protocol.ErrBadRequest, Message: err.Error()})
			continue
		}
		switch env.Type {
		case protocol.TypeUnregister:
			s.rooms.Remove(created.Code)
			log.Info("room released")
			return
		default:
			// Unknown types are ignored so newer agents keep working with an
			// older relay.
			log.Debug("ignoring control message", "type", env.Type)
		}
	}
}

func (s *Server) authorized(token string) bool {
	if token == "" {
		return false
	}
	_, ok := s.cfg.Tokens[token]
	return ok
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// peer serialises writes to a single websocket connection.
type peer struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (p *peer) send(t protocol.Type, payload any) error {
	raw, err := protocol.Encode(t, payload)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}
	return p.conn.WriteMessage(websocket.TextMessage, raw)
}

func (p *peer) pingLoop(ctx context.Context, log *slog.Logger) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			err := p.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
			p.mu.Unlock()
			if err != nil {
				log.Debug("ping failed", "error", err)
				return
			}
		}
	}
}
