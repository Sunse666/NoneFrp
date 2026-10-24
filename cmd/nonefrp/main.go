// Command nonefrp is the single binary used both as the relay server and as
// the host-side agent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Sunse666/NoneFrp/internal/auth"
	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/link"
	"github.com/Sunse666/NoneFrp/internal/logging"
	"github.com/Sunse666/NoneFrp/internal/server"
	"github.com/Sunse666/NoneFrp/internal/supervisor"
	"github.com/Sunse666/NoneFrp/internal/ui"
)

// Build metadata, overridable at link time via -ldflags.
var (
	version   = "0.1.0-dev"
	buildTime = "unknown"
)

// errUsage marks failures caused by bad command line input.
var errUsage = errors.New("usage error")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "nonefrp: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		// Double clicking the binary passes no arguments: that means "run the
		// agent". Everything else is one word away.
		return runClient(nil)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("nonefrp %s (built %s)\n", version, buildTime)
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	case "server":
		return runServer(rest)
	case "client":
		return runClient(rest)
	}
	printUsage()
	return fmt.Errorf("unknown subcommand %q", cmd)
}

func runServer(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "token":
			return runServerToken(args[1:])
		case "link":
			return runServerLink(args[1:])
		case "invite":
			return runServerInvite(args[1:])
		}
	}

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "server.yaml", "path to the server configuration file")
	debug := fs.Bool("debug", false, "log every player connection and rejection")
	check := fs.Bool("check", false, "validate the configuration and exit")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		return err
	}

	if *check {
		fmt.Printf("config ok: %s\n", *configPath)
		fmt.Printf("  listen:        %s\n", cfg.Listen)
		fmt.Printf("  public_port:   %d\n", cfg.PublicPort)
		fmt.Printf("  domain_suffix: %s\n", cfg.DomainSuffix)
		fmt.Printf("  public_url:    %s\n", cfg.PublicURL)
		fmt.Printf("  tokens:        %d\n", len(cfg.Tokens))
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := newLogger(*debug)
	srv := server.New(cfg, logger)
	srv.SetStateFile(filepath.Join(filepath.Dir(*configPath), "state.json"))

	// SIGHUP re-reads the credential table, so issuing an invite does not
	// require a restart that disconnects every host.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for range hup {
			reloaded, err := config.LoadServer(*configPath)
			if err != nil {
				logger.Warn("reload failed, keeping the previous credentials", "error", err)
				continue
			}
			srv.ReloadTokens(reloaded.Tokens)
			logger.Info("credentials reloaded", "tokens", len(reloaded.Tokens))
		}
	}()

	return srv.Run(ctx)
}

func runServerToken(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nonefrp server token <new|ls>")
	}
	switch args[0] {
	case "new":
		fs := flag.NewFlagSet("server token new", flag.ContinueOnError)
		configPath := fs.String("config", "server.yaml", "path to the server configuration file")
		note := fs.String("note", "", "free-form note stored next to the token")
		if err := fs.Parse(args[1:]); err != nil {
			return errUsage
		}

		token, err := auth.GenerateToken()
		if err != nil {
			return err
		}
		if err := auth.AppendTokenFile(*configPath, token, *note); err != nil {
			return err
		}
		fmt.Printf("token: %s\n", token)
		fmt.Printf("written to: %s\n", *configPath)
		fmt.Printf("restart the relay to accept it\n")
		return nil

	case "ls":
		fs := flag.NewFlagSet("server token ls", flag.ContinueOnError)
		configPath := fs.String("config", "server.yaml", "path to the server configuration file")
		if err := fs.Parse(args[1:]); err != nil {
			return errUsage
		}

		cfg, err := config.LoadServer(*configPath)
		if err != nil {
			return err
		}
		if len(cfg.Tokens) == 0 {
			fmt.Println("no tokens issued yet")
			return nil
		}
		codes := make([]string, 0, len(cfg.Tokens))
		for code := range cfg.Tokens {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		counts := server.LoadUsage(filepath.Join(filepath.Dir(*configPath), "state.json"))
		for _, code := range cfg.SortedCodes() {
			credential := cfg.Tokens[code]
			fmt.Printf("%s  %-38s %s\n", code, credential.Usage(counts[code]), credential.Note)
		}
		return nil

	default:
		return fmt.Errorf("unknown token command %q (want new or ls)", args[0])
	}
}

func runServerLink(args []string) error {
	fs := flag.NewFlagSet("server link", flag.ContinueOnError)
	configPath := fs.String("config", "server.yaml", "path to the server configuration file")
	token := fs.String("token", "", "access token to embed in the link")
	relayURL := fs.String("relay", "", "relay url, defaults to public_url in the config")
	room := fs.String("room", "auto", "room code the agent should request")
	all := fs.Bool("all", false, "print a link for every issued token")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		return err
	}
	if !*all && *token == "" {
		return fmt.Errorf("the -token flag is required (or -all for every token)")
	}
	if *token != "" {
		if _, ok := cfg.Tokens[*token]; !ok {
			return fmt.Errorf("token %q has not been issued by this relay", *token)
		}
	}

	target := *relayURL
	if target == "" {
		target = cfg.PublicURL
	}
	if target == "" {
		return fmt.Errorf("no relay url: set public_url in %s or pass -relay", *configPath)
	}

	if *all {
		codes := make([]string, 0, len(cfg.Tokens))
		for code := range cfg.Tokens {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		if len(codes) == 0 {
			return fmt.Errorf("no tokens issued yet: run nonefrp server token new")
		}
		for _, code := range codes {
			built, err := link.JoinLink(target, code, *room)
			if err != nil {
				return err
			}
			fmt.Printf("# %s\n%s\n", cfg.Tokens[code].Note, built)
		}
		return nil
	}

	built, err := link.JoinLink(target, *token, *room)
	if err != nil {
		return err
	}
	fmt.Println(built)
	return nil
}

func runServerInvite(args []string) error {
	fs := flag.NewFlagSet("server invite", flag.ContinueOnError)
	configPath := fs.String("config", "server.yaml", "path to the server configuration file")
	note := fs.String("note", "", "who the invite is for")
	once := fs.Bool("once", false, "allow a single device")
	devices := fs.Int("devices", 0, "how many devices may use it, 0 means unlimited")
	days := fs.Int("days", 0, "days until it expires, 0 means never")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if *once && *devices > 0 {
		return fmt.Errorf("use either -once or -devices, not both")
	}

	credential := config.Token{Note: *note}
	switch {
	case *once:
		credential.MaxDevices = 1
	case *devices > 0:
		credential.MaxDevices = *devices
	}
	if *days > 0 {
		credential.Expires = time.Now().AddDate(0, 0, *days).Truncate(time.Second)
	}

	code, err := auth.GenerateInvite()
	if err != nil {
		return err
	}
	if err := auth.PutCredential(*configPath, code, credential); err != nil {
		return err
	}

	fmt.Printf("invite:  %s\n", code)
	fmt.Printf("policy:  %s\n", credential.Usage(0))
	fmt.Printf("written: %s\n", *configPath)
	fmt.Printf("apply:   restart the relay to accept it\n")
	return nil
}

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	configPath := fs.String("config", defaultClientConfigPath(), "path to the client configuration file")
	relay := fs.String("relay", "", "override the relay url for this run")
	token := fs.String("token", "", "override the access token for this run")
	debug := fs.Bool("debug", false, "log every forwarded stream")
	join := fs.String("join", "", "import a nonefrp://join link and save it")
	check := fs.Bool("check", false, "validate the configuration and exit")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	cfg, err := config.LoadClient(*configPath)
	firstRun := false
	if err != nil {
		// -check is used by scripts: a missing file there is still an error.
		if !errors.Is(err, os.ErrNotExist) || *check {
			return err
		}
		// No configuration yet: start empty and let the web interface collect
		// the relay and the token. Double clicking the binary is a valid way to
		// get started now.
		cfg = config.DefaultClient()
		firstRun = *join == ""
	}
	if *relay != "" {
		cfg.Relay = []config.Relay{{Name: "flag", URL: *relay}}
	}
	if *token != "" {
		cfg.Token = *token
	}

	logger, closeLog := logging.New(*debug, filepath.Join(filepath.Dir(*configPath), "nonefrp.log"))
	defer closeLog()

	if *join != "" {
		request, err := link.ParseJoin(*join)
		if err != nil {
			return err
		}
		cfg.Relay = []config.Relay{{Name: "join-link", URL: request.RelayURL}}
		cfg.Token = request.Token
		if request.Room != "" {
			cfg.Room.Code = request.Room
		}
		if err := config.SaveClient(*configPath, cfg); err != nil {
			return err
		}
		logger.Info("settings imported from a join link", "relay", request.RelayURL, "config", *configPath)
	}

	// A first run has nothing to validate yet; the web interface collects it.
	if err := cfg.Validate(); err != nil && !firstRun {
		return err
	}

	if *check {
		fmt.Printf("config ok: %s\n", *configPath)
		for i, relay := range cfg.Relay {
			fmt.Printf("  relay[%d]:      %s\n", i, relay.URL)
		}
		fmt.Printf("  local_port:    %d\n", cfg.Local.Port)
		fmt.Printf("  room_code:     %s\n", cfg.Room.Code)
		fmt.Printf("  token:         %s\n", mask(cfg.Token))
		return nil
	}

	runner := supervisor.New(*configPath, filepath.Dir(*configPath), cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner.Start(ctx)
	defer runner.Stop()

	if cfg.UI.Enabled {
		webUI, err := ui.New(runner, runner, "", logger)
		if err != nil {
			return err
		}
		base, err := webUI.Start(ctx, "127.0.0.1:0")
		if err != nil {
			return err
		}
		fmt.Printf("web ui: %s\n", base)
		if firstRun {
			fmt.Printf("no configuration yet: use the web interface to paste a join link\n")
			fmt.Printf("or to enter a relay address and a token. it will be saved to %s\n", *configPath)
		}
		if cfg.UI.OpenBrowser {
			if err := ui.OpenBrowser(base); err != nil {
				logger.Warn("could not open a browser", "error", err)
			}
		}
	}

	<-ctx.Done()
	return nil
}

// defaultClientConfigPath returns client.yaml next to the binary, so double
// clicking the executable finds its configuration regardless of the working
// directory a shortcut or a scheduled task hands it.
func defaultClientConfigPath() string {
	executable, err := os.Executable()
	if err != nil {
		return "client.yaml"
	}
	return filepath.Join(filepath.Dir(executable), "client.yaml")
}

// mask hides most of a token so it is safe to print.
func mask(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:8] + strings.Repeat("*", len(token)-8)
}

func newLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func printUsage() {
	fmt.Fprint(os.Stderr, `nonefrp - reverse tunnel for Minecraft servers

Usage:
  nonefrp                 run the host agent (same as "nonefrp client")
  nonefrp <command> [flags]

Commands:
  server              run the relay server
  server token new    issue an access token and store it in the config
  server token ls     list issued access tokens
  server link         print a join link for a token
  server invite       issue an invite code, optionally single device or expiring
  client              run the host agent
  version             print version information
  help                print this message

Run "nonefrp <command> -h" for command specific flags.
`)
}
