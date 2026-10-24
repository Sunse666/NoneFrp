// Package config loads and validates the relay and agent configuration files.
//
// Precedence, lowest to highest: built-in defaults, YAML file, NONERP_*
// environment variables. Command line flags are applied by the caller.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from YAML strings such as "60s".
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("decode duration: %w", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

// Std returns the duration as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Limits bounds the resources a single room may consume.
type Limits struct {
	MaxPlayersPerRoom int `yaml:"max_players_per_room"`
	MaxStreamsPerRoom int `yaml:"max_streams_per_room"`
	ConnRatePerIP     int `yaml:"conn_rate_per_ip"`
}

// Server is the relay server configuration.
type Server struct {
	Listen     string `yaml:"listen"`
	PublicPort int    `yaml:"public_port"`
	// PublicURL is the websocket address agents connect to, used to build join
	// links. It is normally the reverse proxy endpoint, e.g. wss://relay.example.com.
	PublicURL    string           `yaml:"public_url"`
	DomainSuffix string           `yaml:"domain_suffix"`
	GracePeriod  Duration         `yaml:"grace_period"`
	Limits       Limits           `yaml:"limits"`
	Tokens       map[string]Token `yaml:"tokens"`
}

// Relay is one relay endpoint the agent may connect to.
type Relay struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// Local describes the Minecraft server the agent forwards to.
type Local struct {
	Port     int  `yaml:"port"`
	AllowLAN bool `yaml:"allow_lan"`
	// ProxyProtocol prefixes every forwarded connection with a PROXY protocol
	// header so the local server sees the real player address. It must be
	// enabled on the server side too (Paper/Spigot: proxy-protocol: true).
	ProxyProtocol bool `yaml:"proxy_protocol"`
}

// Room describes how the agent asks for a room.
type Room struct {
	Code string `yaml:"code"`
}

// UI configures the local web interface.
type UI struct {
	Enabled     bool `yaml:"enabled"`
	OpenBrowser bool `yaml:"open_browser"`
}

// Client is the host agent configuration.
type Client struct {
	Relay []Relay `yaml:"relay"`
	Local Local   `yaml:"local"`
	Room  Room    `yaml:"room"`
	Token string  `yaml:"token"`
	UI    UI      `yaml:"ui"`
}

// DefaultServer returns the built-in relay defaults.
func DefaultServer() Server {
	return Server{
		Listen:       "127.0.0.1:8080",
		PublicPort:   25565,
		DomainSuffix: "",
		GracePeriod:  Duration(60 * time.Second),
		Limits: Limits{
			MaxPlayersPerRoom: 10,
			MaxStreamsPerRoom: 64,
			ConnRatePerIP:     20,
		},
		Tokens: map[string]Token{},
	}
}

// DefaultClient returns the built-in agent defaults.
func DefaultClient() Client {
	return Client{
		Relay: nil,
		Local: Local{Port: 25565, AllowLAN: false},
		Room:  Room{Code: "auto"},
		UI:    UI{Enabled: true, OpenBrowser: true},
	}
}

// LoadServer reads a relay configuration file.
func LoadServer(path string) (Server, error) {
	cfg := DefaultServer()
	if err := decodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// LoadClient reads an agent configuration file and applies environment overrides.
func LoadClient(path string) (Client, error) {
	cfg := DefaultClient()
	if err := decodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	applyClientEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func decodeFile(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return nil
}

func applyClientEnv(cfg *Client) {
	if v := os.Getenv("NONERP_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("NONERP_RELAY"); v != "" {
		cfg.Relay = nil
		for i, raw := range strings.Split(v, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			cfg.Relay = append(cfg.Relay, Relay{Name: fmt.Sprintf("env-%d", i+1), URL: raw})
		}
	}
	if v := os.Getenv("NONERP_LOCAL_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Local.Port = port
		}
	}
	if v := os.Getenv("NONERP_ROOM_CODE"); v != "" {
		cfg.Room.Code = v
	}
}

// Validate checks the relay configuration for obvious mistakes.
func (s Server) Validate() error {
	if strings.TrimSpace(s.Listen) == "" {
		return fmt.Errorf("listen address must not be empty")
	}
	if s.PublicPort < 1 || s.PublicPort > 65535 {
		return fmt.Errorf("public_port %d is out of range", s.PublicPort)
	}
	if strings.TrimSpace(s.DomainSuffix) == "" {
		return fmt.Errorf("domain_suffix must not be empty")
	}
	if s.GracePeriod.Std() < 0 {
		return fmt.Errorf("grace_period must not be negative")
	}
	if s.PublicURL != "" && !strings.HasPrefix(s.PublicURL, "ws://") && !strings.HasPrefix(s.PublicURL, "wss://") {
		return fmt.Errorf("public_url %q must start with ws:// or wss://", s.PublicURL)
	}
	return nil
}

// Validate checks the agent configuration for obvious mistakes.
func (c Client) Validate() error {
	if len(c.Relay) == 0 {
		return fmt.Errorf("at least one relay endpoint is required")
	}
	for _, r := range c.Relay {
		if err := validateRelayURL(r.URL); err != nil {
			return err
		}
	}
	if c.Local.Port < 1 || c.Local.Port > 65535 {
		return fmt.Errorf("local.port %d is out of range", c.Local.Port)
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("token must not be empty")
	}
	return nil
}

// NormalizeRelayURL accepts what a person would type into a form: a bare
// hostname is assumed to be a secure websocket endpoint.
func NormalizeRelayURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		return trimmed
	}
	return "wss://" + strings.TrimSuffix(trimmed, "/")
}

func validateRelayURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("relay url must not be empty")
	}
	if !strings.HasPrefix(raw, "ws://") && !strings.HasPrefix(raw, "wss://") {
		return fmt.Errorf("relay url %q must start with ws:// or wss://", raw)
	}
	return nil
}

// RoomHost joins a room code and the configured domain suffix.
func (s Server) RoomHost(roomCode string) string {
	return roomCode + "." + s.DomainSuffix
}
