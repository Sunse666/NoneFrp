package ui

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sunse666/NoneFrp/internal/client"
)

type fakeSource struct {
	status client.Status
}

func (f fakeSource) Status() client.Status { return f.status }

func newTestUI(t *testing.T, status client.Status) (*httptest.Server, *Server) {
	t.Helper()
	srv, err := New(fakeSource{status: status}, "testtoken", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new ui: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

func TestIndexRenders(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{})

	resp, err := http.Get(ts.URL + "/testtoken/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	page := string(body)
	for _, want := range []string{"房间地址", "在线玩家", "/testtoken"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page does not contain %q", want)
		}
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q", ct)
	}
}

func TestStatusEndpoint(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{
		Connected:  true,
		RoomCode:   "a2b3c4",
		PublicHost: "a2b3c4.mc.example.com",
		PublicPort: 25565,
		Players:    2,
		BytesUp:    100,
		BytesDown:  2048,
	})

	resp, err := http.Get(ts.URL + "/testtoken/api/status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var got client.Status
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Connected || got.RoomCode != "a2b3c4" || got.Players != 2 {
		t.Fatalf("status = %+v", got)
	}
	if got.BytesDown != 2048 {
		t.Fatalf("bytes down = %d", got.BytesDown)
	}
}

func TestWrongTokenIsNotFound(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{})

	resp, err := http.Get(ts.URL + "/wrongtoken/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestForeignHostIsForbidden(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{})

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/testtoken/", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// A page on another origin would arrive with its own Host header.
	req.Host = "evil.example.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRandomTokenIsGenerated(t *testing.T) {
	first, err := New(fakeSource{}, "", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	second, err := New(fakeSource{}, "", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if len(first.Token()) != 32 {
		t.Fatalf("token length = %d", len(first.Token()))
	}
	if first.Token() == second.Token() {
		t.Fatal("two interfaces must not share a token")
	}
}
