// Command nonerp is the single binary used both as the relay server and as
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
	"syscall"

	"github.com/Sunse666/NoneFrp/internal/auth"
	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/link"
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
		fmt.Fprintf(os.Stderr, "nonerp: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("nonerp %s (built %s)\n", version, buildTime)
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
		}
	}

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "server.yaml", "path to the server configuration file")
	debug := fs.Bool("debug", false, "log every player connection and rejection")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.New(cfg, newLogger(*debug)).Run(ctx)
}

func runServerToken(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nonerp server token <new|ls>")
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
		for _, code := range codes {
			fmt.Printf("%s\t%s\n", code, cfg.Tokens[code])
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
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		return err
	}
	if *token == "" {
		return fmt.Errorf("the -token flag is required")
	}
	if _, ok := cfg.Tokens[*token]; !ok {
		return fmt.Errorf("token %q has not been issued by this relay", *token)
	}

	target := *relayURL
	if target == "" {
		target = cfg.PublicURL
	}
	if target == "" {
		return fmt.Errorf("no relay url: set public_url in %s or pass -relay", *configPath)
	}

	built, err := link.JoinLink(target, *token, *room)
	if err != nil {
		return err
	}
	fmt.Println(built)
	return nil
}

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	configPath := fs.String("config", "client.yaml", "path to the client configuration file")
	relay := fs.String("relay", "", "override the relay url for this run")
	token := fs.String("token", "", "override the access token for this run")
	debug := fs.Bool("debug", false, "log every forwarded stream")
	join := fs.String("join", "", "import a nonerp://join link and save it")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	cfg, err := config.LoadClient(*configPath)
	if err != nil {
		return err
	}
	if *relay != "" {
		cfg.Relay = []config.Relay{{Name: "flag", URL: *relay}}
	}
	if *token != "" {
		cfg.Token = *token
	}

	logger := newLogger(*debug)

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

	if err := cfg.Validate(); err != nil {
		return err
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
		if cfg.UI.OpenBrowser {
			if err := ui.OpenBrowser(base); err != nil {
				logger.Warn("could not open a browser", "error", err)
			}
		}
	}

	<-ctx.Done()
	return nil
}

func newLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func printUsage() {
	fmt.Fprint(os.Stderr, `nonerp - reverse tunnel for Minecraft servers

Usage:
  nonerp <command> [flags]

Commands:
  server              run the relay server
  server token new    issue an access token and store it in the config
  server token ls     list issued access tokens
  server link         print a join link for a token
  client              run the host agent
  version             print version information
  help                print this message

Run "nonerp <command> -h" for command specific flags.
`)
}
