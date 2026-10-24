package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sunse666/NoneFrp/internal/auth"
	"github.com/Sunse666/NoneFrp/internal/channel"
	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/mcproto"
	"github.com/Sunse666/NoneFrp/internal/protocol"
	"github.com/Sunse666/NoneFrp/internal/proxy"
)

const (
	// handshakeTimeout bounds how long a player may take to send its handshake.
	handshakeTimeout = 10 * time.Second
	// openStreamTimeout bounds how long the relay waits for the agent to accept.
	openStreamTimeout = 10 * time.Second
	// dialerWait is how long a player waits for a room data session to appear.
	dialerWait = 2 * time.Second
	// rejectTimeout bounds how long a rejection may take to deliver.
	rejectTimeout = 10 * time.Second
)

// dataplane tracks the live data sessions of all rooms.
type dataplane struct {
	mu      sync.Mutex
	dials   map[string]channel.Dialer
	streams map[string]int
	players map[string]int
	control map[string]*peer
}

func newDataplane() *dataplane {
	return &dataplane{
		dials:   make(map[string]channel.Dialer),
		streams: make(map[string]int),
		players: make(map[string]int),
		control: make(map[string]*peer),
	}
}

// setControl records the control connection of a room so player events can be
// pushed to the agent.
func (p *dataplane) setControl(code string, writer *peer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.control[code] = writer
}

// dropControl forgets the control connection of a room.
func (p *dataplane) dropControl(code string, writer *peer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if current, ok := p.control[code]; ok && current == writer {
		delete(p.control, code)
	}
}

// notify sends an advisory message to a room's agent. Failure is ignored: these
// messages only drive the status display.
func (p *dataplane) notify(code string, messageType protocol.Type, payload any) {
	p.mu.Lock()
	target, ok := p.control[code]
	p.mu.Unlock()
	if !ok {
		return
	}
	_ = target.send(messageType, payload)
}

// setDialer installs the data session of a room, replacing and closing any
// previous one.
func (p *dataplane) setDialer(code string, dialer channel.Dialer) {
	p.mu.Lock()
	previous, had := p.dials[code]
	p.dials[code] = dialer
	p.mu.Unlock()
	if had && previous != dialer {
		_ = previous.Close()
	}
}

// dropDialer removes the data session of a room if it is still the given one.
func (p *dataplane) dropDialer(code string, dialer channel.Dialer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if current, ok := p.dials[code]; ok && current == dialer {
		delete(p.dials, code)
		delete(p.streams, code)
	}
}

// dialer returns the live data session of a room.
func (p *dataplane) dialer(code string) (channel.Dialer, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	found, ok := p.dials[code]
	return found, ok
}

// waitDialer returns the data session of a room, waiting briefly for one to
// appear so that a player connecting in the instant after registration is not
// dropped.
func (p *dataplane) waitDialer(code string, timeout time.Duration) (channel.Dialer, bool) {
	if found, ok := p.dialer(code); ok {
		return found, true
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return nil, false
		case <-ticker.C:
			if found, ok := p.dialer(code); ok {
				return found, true
			}
		}
	}
}

// acquire reserves a slot, honouring both the stream and the player limits.
// Server list pings count against the stream limit only.
func (p *dataplane) acquire(code string, limits config.Limits, player bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if limits.MaxStreamsPerRoom > 0 && p.streams[code] >= limits.MaxStreamsPerRoom {
		return false
	}
	if player && limits.MaxPlayersPerRoom > 0 && p.players[code] >= limits.MaxPlayersPerRoom {
		return false
	}
	p.streams[code]++
	if player {
		p.players[code]++
	}
	return true
}

// release returns a slot taken by acquire.
func (p *dataplane) release(code string, player bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.streams[code] > 1 {
		p.streams[code]--
	} else {
		delete(p.streams, code)
	}
	if !player {
		return
	}
	if p.players[code] > 1 {
		p.players[code]--
	} else {
		delete(p.players, code)
	}
}

// retainOnly closes data sessions of rooms that no longer exist.
func (p *dataplane) retainOnly(alive map[string]bool) {
	p.mu.Lock()
	stale := make([]channel.Dialer, 0, len(p.dials))
	for code, dialer := range p.dials {
		if alive[code] {
			continue
		}
		stale = append(stale, dialer)
		delete(p.dials, code)
		delete(p.streams, code)
	}
	p.mu.Unlock()

	for _, dialer := range stale {
		_ = dialer.Close()
	}
}

// closeAll shuts down every data session.
func (p *dataplane) closeAll() {
	p.mu.Lock()
	all := make([]channel.Dialer, 0, len(p.dials))
	for _, dialer := range p.dials {
		all = append(all, dialer)
	}
	p.dials = make(map[string]channel.Dialer)
	p.mu.Unlock()

	for _, dialer := range all {
		_ = dialer.Close()
	}
}

// listenPublic binds the port players connect to.
func (s *Server) listenPublic() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.PublicPort))
	if err != nil {
		return fmt.Errorf("listen on the public port %d: %w", s.cfg.PublicPort, err)
	}
	s.publicListener = ln
	return nil
}

// PlayerAddr returns the address players connect to.
func (s *Server) PlayerAddr() net.Addr {
	if s.publicListener == nil {
		return nil
	}
	return s.publicListener.Addr()
}

// ServePublic binds the public port and serves player connections until ctx is
// cancelled. Run calls it automatically; it is exported so tests and embedders
// can run the data plane without the control listener.
func (s *Server) ServePublic(ctx context.Context) error {
	if err := s.listenPublic(); err != nil {
		return err
	}
	s.servePublic(ctx)
	return nil
}

func (s *Server) servePublic(ctx context.Context) {
	go func() {
		<-ctx.Done()
		_ = s.publicListener.Close()
	}()

	for {
		conn, err := s.publicListener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			s.log.Warn("player listener stopped", "error", err)
			return
		}
		go s.handlePlayer(conn)
	}
}

// handleData upgrades an agent connection into the data session of a room.
func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(bearerToken(r)) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	query := r.URL.Query()
	roomCode := query.Get("room")
	deviceID := query.Get("device")

	tracked, ok := s.rooms.Get(roomCode)
	if !ok {
		http.Error(w, "unknown room", http.StatusNotFound)
		return
	}
	if tracked.DeviceID != deviceID {
		http.Error(w, "room does not belong to this device", http.StatusForbidden)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("data upgrade failed", "room", roomCode, "error", err)
		return
	}

	dialer, err := channel.NewYamuxDialer(channel.NewWSConn(conn), channel.YamuxConfig())
	if err != nil {
		s.log.Warn("data session failed", "room", roomCode, "error", err)
		_ = conn.Close()
		return
	}

	s.plane.setDialer(roomCode, dialer)
	s.stats.sessionOpen()
	log := s.log.With("room", roomCode)
	log.Info("data session established")

	<-dialer.Done()

	s.stats.sessionClosed()
	s.plane.dropDialer(roomCode, dialer)
	_ = dialer.Close()
	log.Info("data session closed")
}

// handlePlayer routes one player connection to the agent that owns the room
// named in the handshake.
func (s *Server) handlePlayer(conn net.Conn) {
	traceID := newTraceID()
	clientIP, clientPort := splitHostPort(conn.RemoteAddr())
	log := s.log.With("trace", traceID, "player", conn.RemoteAddr().String())

	if !s.limiter.allow(clientIP) {
		// Scanners get no protocol reply at all, just a closed connection.
		log.Debug("dropping player: rate limit exceeded")
		_ = conn.Close()
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	handshake, err := mcproto.ReadHandshake(conn)
	if err != nil {
		log.Debug("dropping player: unreadable handshake", "error", err)
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	roomCode, ok := roomCodeFromHost(handshake.ServerAddress, s.cfg.DomainSuffix)
	if !ok {
		log.Debug("dropping player: host is outside the room domain", "host", handshake.ServerAddress)
		_ = conn.Close()
		return
	}
	if _, ok := s.rooms.Get(roomCode); !ok {
		log.Debug("rejecting player: unknown room", "host", handshake.ServerAddress)
		s.stats.rejected("room_not_found")
		s.rejectPlayer(conn, handshake, messageRoomNotFound)
		return
	}
	dialer, ok := s.plane.waitDialer(roomCode, dialerWait)
	if !ok {
		log.Debug("rejecting player: room has no data session", "room", roomCode)
		s.stats.rejected("host_offline")
		s.rejectPlayer(conn, handshake, messageHostOffline)
		return
	}
	isPlayer := handshake.NextState == mcproto.NextStateLogin
	if !s.plane.acquire(roomCode, s.cfg.Limits, isPlayer) {
		log.Warn("rejecting player: room is full", "room", roomCode, "login", isPlayer)
		s.stats.rejected("room_full")
		s.rejectPlayer(conn, handshake, messageRoomFull)
		return
	}
	if isPlayer {
		s.stats.playerJoined()
		defer s.stats.playerLeft()
	}
	defer s.plane.release(roomCode, isPlayer)

	s.plane.notify(roomCode, protocol.TypePeerJoined, protocol.PeerJoined{
		PlayerIP: clientIP,
		At:       time.Now().UTC().Format(time.RFC3339),
	})
	defer s.plane.notify(roomCode, protocol.TypePeerLeft, protocol.PeerLeft{
		PlayerIP: clientIP,
		At:       time.Now().UTC().Format(time.RFC3339),
	})

	meta := protocol.StreamMeta{
		Host:      handshake.ServerAddress,
		NextState: handshake.NextState,
		RoomCode:  roomCode,
		TraceID:   traceID,
	}
	meta.ClientIP, meta.ClientPort = clientIP, clientPort

	ctx, cancel := context.WithTimeout(context.Background(), openStreamTimeout)
	defer cancel()

	stream, err := dialer.Open(ctx, meta)
	if err != nil {
		log.Warn("opening a player stream failed", "room", roomCode, "error", err)
		_ = conn.Close()
		return
	}

	// The agent never saw the handshake, so replay the exact bytes it consumed.
	if _, err := stream.Write(handshake.Raw); err != nil {
		log.Warn("replaying the handshake failed", "room", roomCode, "error", err)
		_ = stream.Close()
		_ = conn.Close()
		return
	}

	options := proxy.Options{
		Idle:             proxy.DefaultIdleTimeout,
		CloseOnServerEOF: true,
	}
	if handshake.NextState == mcproto.NextStateLogin {
		// A player who closes the connection mid-game is gone; do not wait for
		// the server to notice.
		options.CloseOnClientEOF = true
		options.Idle = proxy.LoginIdleTimeout
	}
	result := proxy.SpliceWith(conn, stream, options)
	s.stats.transferred(result.Up, result.Down)
	log.Debug("player connection finished",
		"room", roomCode,
		"up", result.Up,
		"down", result.Down,
		"error", result.Err,
	)
}

// Player facing rejection messages. These are the only strings a player sees,
// so they are written in the language of the player, not the developer.
const (
	messageRoomNotFound = "房间不存在，请检查地址是否输入正确"
	messageHostOffline  = "房主暂时不在线，请稍后再试"
	messageRoomFull     = "房间已满，请稍后再试"
)

// rejectPlayer explains why a player cannot be routed. A server list ping gets
// a synthetic entry so the reason is visible in the multiplayer screen, and a
// login attempt gets a normal disconnect screen.
func (s *Server) rejectPlayer(conn net.Conn, handshake mcproto.Handshake, message string) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(rejectTimeout))

	if handshake.NextState != mcproto.NextStateStatus {
		if err := mcproto.WriteLoginDisconnect(conn, message); err != nil {
			s.log.Debug("sending a login rejection failed", "error", err)
		}
		return
	}

	// The protocol version must be echoed back, otherwise clients report a
	// version mismatch instead of showing the message.
	reply := mcproto.StatusReply{
		ProtocolVersion: handshake.ProtocolVersion,
		VersionName:     "NoneFrp",
		Description:     message,
		Online:          0,
		Max:             0,
	}
	if err := mcproto.ServeStatusReply(conn, reply); err != nil {
		s.log.Debug("sending a status rejection failed", "error", err)
	}
}

// roomCodeFromHost extracts the room code from a handshake host such as
// "a2b3c4.mc.example.com" or "a2b3c4-map.mc.example.com".
func roomCodeFromHost(host, suffix string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(host))
	normalized = strings.TrimSuffix(normalized, ".")
	if idx := strings.IndexByte(normalized, ':'); idx >= 0 {
		normalized = normalized[:idx]
	}

	suffix = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(suffix), "."))
	if suffix == "" || !strings.HasSuffix(normalized, "."+suffix) {
		return "", false
	}

	label := strings.TrimSuffix(normalized, "."+suffix)
	room, _, _ := strings.Cut(label, "-")

	code, err := auth.NormalizeRoomCode(room)
	if err != nil {
		return "", false
	}
	return code, true
}

func splitHostPort(addr net.Addr) (string, int) {
	if addr == nil {
		return "", 0
	}
	host, portRaw, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String(), 0
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		return host, 0
	}
	return host, port
}

func newTraceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}
