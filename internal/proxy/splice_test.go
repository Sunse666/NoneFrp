package proxy

import (
	"io"
	"net"
	"testing"
	"time"
)

// dialPair returns two connected TCP sockets. TCP is used instead of net.Pipe
// because only real sockets support CloseWrite.
func dialPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	select {
	case server := <-accepted:
		t.Cleanup(func() {
			_ = client.Close()
			_ = server.Close()
		})
		return client, server
	case err := <-acceptErr:
		t.Fatalf("accept: %v", err)
	}
	return nil, nil
}

func TestSpliceCopiesBothDirections(t *testing.T) {
	clientA, spliceA := dialPair(t)
	spliceB, serverB := dialPair(t)

	go Splice(spliceA, spliceB, 0)

	if _, err := clientA.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	_ = serverB.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(serverB, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q", buf)
	}

	if _, err := serverB.Write([]byte("world")); err != nil {
		t.Fatalf("write back: %v", err)
	}
	_ = clientA.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(clientA, buf); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(buf) != "world" {
		t.Fatalf("got %q", buf)
	}
}

func TestSplicePropagatesHalfClose(t *testing.T) {
	client, spliceA := dialPair(t)
	spliceB, server := dialPair(t)

	done := make(chan Result, 1)
	go func() { done <- Splice(spliceA, spliceB, 0) }()

	// The client sends its request and shuts down its write side, exactly like
	// a server list ping.
	if _, err := client.Write([]byte("status request")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatalf("close write: %v", err)
	}

	_ = server.SetReadDeadline(time.Now().Add(3 * time.Second))
	request, err := io.ReadAll(server)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if string(request) != "status request" {
		t.Fatalf("server read %q", request)
	}

	// Only a propagated FIN makes ReadAll return here; the reply must still go
	// back through the other direction.
	if _, err := server.Write([]byte("status response")); err != nil {
		t.Fatalf("write response: %v", err)
	}
	_ = server.Close()

	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(response) != "status response" {
		t.Fatalf("client read %q", response)
	}

	select {
	case res := <-done:
		if res.Up != int64(len("status request")) {
			t.Fatalf("up = %d", res.Up)
		}
		if res.Down != int64(len("status response")) {
			t.Fatalf("down = %d", res.Down)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("splice did not return")
	}
}

func TestSpliceTimesOutIdleConnections(t *testing.T) {
	clientA, spliceA := dialPair(t)
	spliceB, serverB := dialPair(t)
	_, _ = clientA, serverB

	start := time.Now()
	res := Splice(spliceA, spliceB, 150*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Fatalf("splice took %v", elapsed)
	}
	if !res.Timeout() {
		t.Fatalf("err = %v, want a timeout", res.Err)
	}
}

func TestSpliceWithEndsWhenUpstreamCloses(t *testing.T) {
	clientA, spliceA := dialPair(t)
	spliceB, serverB := dialPair(t)

	done := make(chan Result, 1)
	go func() {
		done <- SpliceWith(spliceA, spliceB, Options{CloseOnUpstreamEOF: true})
	}()

	// The client keeps its side wide open, exactly like a kicked player that
	// does not notice. The server closing must still end the pair.
	if _, err := clientA.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	_ = serverB.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(serverB, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	_ = serverB.Close()

	select {
	case res := <-done:
		if res.Up != 5 {
			t.Fatalf("up = %d", res.Up)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("splice kept waiting after the upstream closed")
	}

	// The client must see the connection go away instead of hanging.
	_ = clientA.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := clientA.Read(buf)
	if err == nil {
		t.Fatal("expected the client connection to be closed")
	}
}

func TestSpliceWithStillForwardsClientHalfClose(t *testing.T) {
	client, spliceA := dialPair(t)
	spliceB, server := dialPair(t)

	done := make(chan Result, 1)
	go func() {
		done <- SpliceWith(spliceA, spliceB, Options{CloseOnUpstreamEOF: true})
	}()

	if _, err := client.Write([]byte("status request")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatalf("close write: %v", err)
	}

	_ = server.SetReadDeadline(time.Now().Add(3 * time.Second))
	request, err := io.ReadAll(server)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if string(request) != "status request" {
		t.Fatalf("server read %q", request)
	}

	// The client is done writing, but the reply direction must stay usable.
	if _, err := server.Write([]byte("status response")); err != nil {
		t.Fatalf("write response: %v", err)
	}
	_ = server.Close()

	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(response) != "status response" {
		t.Fatalf("client read %q", response)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("splice did not return")
	}
}

func TestSpliceReturnsByteCounts(t *testing.T) {
	clientA, spliceA := dialPair(t)
	spliceB, serverB := dialPair(t)

	done := make(chan Result, 1)
	go func() { done <- Splice(spliceA, spliceB, 0) }()

	payload := []byte("0123456789")
	if _, err := clientA.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = clientA.Close()
	_ = serverB.Close()

	select {
	case res := <-done:
		if res.Up != int64(len(payload)) {
			t.Fatalf("up = %d, want %d", res.Up, len(payload))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("splice did not return")
	}
}
