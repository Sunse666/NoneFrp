package ui

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Sunse666/NoneFrp/internal/client"
	"github.com/Sunse666/NoneFrp/internal/config"
)

type fakeSource struct {
	status client.Status
}

func (f fakeSource) Status() client.Status { return f.status }

type fakeSettings struct {
	cfg     config.Client
	applied *config.Client
	err     error
}

func (f *fakeSettings) Config() config.Client { return f.cfg }

func (f *fakeSettings) Apply(cfg config.Client) error {
	if f.err != nil {
		return f.err
	}
	f.cfg = cfg
	f.applied = &cfg
	return nil
}

func newTestUI(t *testing.T, status client.Status, settings Settings) (*httptest.Server, *Server) {
	t.Helper()
	srv, err := New(fakeSource{status: status}, settings, "testtoken", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new ui: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

func defaultSettings() *fakeSettings {
	cfg := config.DefaultClient()
	cfg.Token = "NFRP-7K2M9QXP"
	cfg.Relay = []config.Relay{{Name: "official", URL: "wss://relay.example.com"}}
	cfg.Local.Port = 25565
	cfg.Room.Code = "auto"
	return &fakeSettings{cfg: cfg}
}

func get(t *testing.T, target string) (int, string) {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatalf("get %s: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestIndexRenders(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{}, defaultSettings())

	code, page := get(t, ts.URL+"/testtoken/")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"房间地址", "在线玩家", "/testtoken", "保存并重连", "wss://relay.example.com"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page does not contain %q", want)
		}
	}
	if strings.Contains(page, "NFRP-7K2M9QXP") {
		t.Fatal("the page leaked the full token")
	}
}

func TestIndexIsReadOnlyWithoutSettings(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{}, nil)

	code, page := get(t, ts.URL+"/testtoken/")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if strings.Contains(page, "保存并重连") {
		t.Fatal("a read only interface must not render the settings form")
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
	}, nil)

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
}

func TestWrongTokenIsNotFound(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{}, defaultSettings())

	code, _ := get(t, ts.URL+"/wrongtoken/")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}

func TestForeignHostIsForbidden(t *testing.T) {
	ts, _ := newTestUI(t, client.Status{}, defaultSettings())

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/testtoken/", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
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

// A brand new install has neither a relay nor a token: the page has to say so
// and offer the join link field instead of an empty status.
func TestUnconfiguredPagePromptsSetup(t *testing.T) {
	settings := &fakeSettings{cfg: config.DefaultClient()}
	ts, _ := newTestUI(t, client.Status{}, settings)

	code, page := get(t, ts.URL+"/testtoken/")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"还没有连上中继", "邀请链接", "保存并重连"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page does not contain %q", want)
		}
	}
}

func TestSaveJoinLinkOverridesFields(t *testing.T) {
	settings := defaultSettings()
	ts, _ := newTestUI(t, client.Status{}, settings)

	form := url.Values{}
	form.Set("csrf", "testtoken")
	form.Set("join", "nonerp://join?relay=wss%3A%2F%2Frelay2.example.com&room=b7k2m9&token=NFRP-NEWTOKEN")
	form.Set("relays", "")
	form.Set("token", "")
	form.Set("local_port", "25565")
	form.Set("room_code", "auto")

	resp, err := http.PostForm(ts.URL+"/testtoken/settings", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if settings.applied == nil {
		t.Fatal("the settings were never applied")
	}
	got := *settings.applied
	if len(got.Relay) != 1 || got.Relay[0].URL != "wss://relay2.example.com" {
		t.Fatalf("relay = %+v", got.Relay)
	}
	if got.Token != "NFRP-NEWTOKEN" {
		t.Fatalf("token = %q", got.Token)
	}
	if got.Room.Code != "b7k2m9" {
		t.Fatalf("room = %q", got.Room.Code)
	}
}

func TestSaveRejectsBadJoinLink(t *testing.T) {
	settings := defaultSettings()
	ts, _ := newTestUI(t, client.Status{}, settings)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{}
	form.Set("csrf", "testtoken")
	form.Set("join", "https://not-a-join-link")
	form.Set("relays", "wss://relay.example.com")
	form.Set("local_port", "25565")

	resp, err := client.PostForm(ts.URL+"/testtoken/settings", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if settings.applied != nil {
		t.Fatal("a rejected link was still applied")
	}
}

func TestSaveSettings(t *testing.T) {
	settings := defaultSettings()
	ts, _ := newTestUI(t, client.Status{}, settings)

	form := url.Values{}
	form.Set("csrf", "testtoken")
	form.Set("relays", "wss://a.example.com\nwss://b.example.com\n")
	form.Set("token", "NFRP-NEWTOKEN")
	form.Set("local_port", "25599")
	form.Set("room_code", "b7k2m9")
	form.Set("proxy_protocol", "on")

	resp, err := http.PostForm(ts.URL+"/testtoken/settings", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	if settings.applied == nil {
		t.Fatal("the settings were never applied")
	}
	got := *settings.applied
	if len(got.Relay) != 2 || got.Relay[1].URL != "wss://b.example.com" {
		t.Fatalf("relay = %+v", got.Relay)
	}
	if got.Token != "NFRP-NEWTOKEN" {
		t.Fatalf("token = %q", got.Token)
	}
	if got.Local.Port != 25599 || !got.Local.ProxyProtocol {
		t.Fatalf("local = %+v", got.Local)
	}
	if got.Room.Code != "b7k2m9" {
		t.Fatalf("room = %q", got.Room.Code)
	}
}

func TestSaveSettingsKeepsTokenWhenBlank(t *testing.T) {
	settings := defaultSettings()
	ts, _ := newTestUI(t, client.Status{}, settings)

	form := url.Values{}
	form.Set("csrf", "testtoken")
	form.Set("relays", "wss://a.example.com")
	form.Set("token", "")
	form.Set("local_port", "25565")
	form.Set("room_code", "auto")

	resp, err := http.PostForm(ts.URL+"/testtoken/settings", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if settings.applied == nil {
		t.Fatal("the settings were never applied")
	}
	if got := settings.applied.Token; got != "NFRP-7K2M9QXP" {
		t.Fatalf("token = %q, want the previous token", got)
	}
}

func TestSaveSettingsRejectsForeignCsrf(t *testing.T) {
	settings := defaultSettings()
	ts, _ := newTestUI(t, client.Status{}, settings)

	form := url.Values{}
	form.Set("csrf", "guessed")
	form.Set("relays", "wss://a.example.com")
	form.Set("local_port", "25565")

	resp, err := http.PostForm(ts.URL+"/testtoken/settings", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if settings.applied != nil {
		t.Fatal("settings were applied without the csrf token")
	}
}

func TestSaveSettingsRejectsBadPort(t *testing.T) {
	settings := defaultSettings()
	ts, _ := newTestUI(t, client.Status{}, settings)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{}
	form.Set("csrf", "testtoken")
	form.Set("relays", "wss://a.example.com")
	form.Set("local_port", "not-a-port")

	resp, err := client.PostForm(ts.URL+"/testtoken/settings", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	if location.Query().Get("err") != "1" {
		t.Fatalf("location = %q, want an error flag", location.String())
	}
	if settings.applied != nil {
		t.Fatal("an invalid port was applied")
	}
}

func TestRandomTokenIsGenerated(t *testing.T) {
	first, err := New(fakeSource{}, nil, "", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	second, err := New(fakeSource{}, nil, "", slog.New(slog.DiscardHandler))
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
