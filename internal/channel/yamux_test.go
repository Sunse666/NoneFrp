package channel

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Sunse666/NoneFrp/internal/mcproto"
	"github.com/Sunse666/NoneFrp/internal/protocol"
)

func newYamuxPair(t *testing.T) (*YamuxDialer, *YamuxAcceptor) {
	t.Helper()
	relaySide, agentSide := net.Pipe()
	dialer, err := NewYamuxDialer(relaySide, YamuxConfig())
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	acceptor, err := NewYamuxAcceptor(agentSide, YamuxConfig())
	if err != nil {
		t.Fatalf("acceptor: %v", err)
	}
	t.Cleanup(func() {
		_ = dialer.Close()
		_ = acceptor.Close()
	})
	return dialer, acceptor
}

func TestYamuxCarriesPrefaceAndPayload(t *testing.T) {
	dialer, acceptor := newYamuxPair(t)

	meta := protocol.StreamMeta{
		ClientIP:   "198.51.100.9",
		ClientPort: 40000,
		Host:       "a2b3c4.mc.example.com",
		NextState:  mcproto.NextStateStatus,
		RoomCode:   "a2b3c4",
		TraceID:    "trace-1",
	}
	payload := []byte("minecraft handshake bytes")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		stream, err := dialer.Open(ctx, meta)
		if err != nil {
			return
		}
		defer func() { _ = stream.Close() }()
		_, _ = stream.Write(payload)
	}()

	stream, got, err := acceptor.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if got != meta {
		t.Fatalf("meta = %+v, want %+v", got, meta)
	}

	_ = stream.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("payload = %q, want %q", buf, payload)
	}
}

func TestYamuxKeepsStreamsSeparate(t *testing.T) {
	dialer, acceptor := newYamuxPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const streams = 4
	var wg sync.WaitGroup
	for i := range streams {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			stream, err := dialer.Open(ctx, protocol.StreamMeta{RoomCode: "a2b3c4", TraceID: string(rune('a' + id))})
			if err != nil {
				return
			}
			defer func() { _ = stream.Close() }()
			_, _ = stream.Write([]byte{byte(id)})
		}(i)
	}

	seen := map[byte]bool{}
	for range streams {
		stream, meta, err := acceptor.Accept()
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		_ = stream.SetReadDeadline(time.Now().Add(2 * time.Second))
		var b [1]byte
		if _, err := io.ReadFull(stream, b[:]); err != nil {
			t.Fatalf("read: %v", err)
		}
		if seen[b[0]] {
			t.Fatalf("stream %d delivered twice", b[0])
		}
		seen[b[0]] = true
		if meta.RoomCode != "a2b3c4" {
			t.Fatalf("wrong preface: %+v", meta)
		}
		_ = stream.Close()
	}
	wg.Wait()

	if len(seen) != streams {
		t.Fatalf("saw %d distinct streams, want %d", len(seen), streams)
	}
}

func TestYamuxAcceptorReportsClosedSession(t *testing.T) {
	dialer, acceptor := newYamuxPair(t)
	_ = dialer.Close()

	done := make(chan error, 1)
	go func() {
		_, _, err := acceptor.Accept()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after the session closed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("accept did not return after the session closed")
	}
}

func TestYamuxReadyIsClosed(t *testing.T) {
	dialer, acceptor := newYamuxPair(t)

	select {
	case <-dialer.Ready():
	default:
		t.Fatal("dialer is not ready")
	}
	select {
	case <-acceptor.Ready():
	default:
		t.Fatal("acceptor is not ready")
	}
}
