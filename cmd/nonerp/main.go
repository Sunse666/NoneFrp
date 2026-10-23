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
	"github.com/Sunse666/NoneFrp/internal/client"
	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/protocol"
	"github.com/Sunse666/NoneFrp/internal/server"
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
	if len(args) > 0 && args[0] == "token" {
		return runServerToken(args[1:])
	}

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "server.yaml", "path to the server configuration file")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.New(cfg, newLogger()).Run(ctx)
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

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	configPath := fs.String("config", "client.yaml", "path to the client configuration file")
	relay := fs.String("relay", "", "override the relay url for this run")
	token := fs.String("token", "", "override the access token for this run")
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
	if err := cfg.Validate(); err != nil {
		return err
	}

	dataDir := filepath.Dir(*configPath)
	agent, err := client.New(cfg, dataDir, newLogger())
	if err != nil {
		return err
	}
	agent.OnRegistered = func(r protocol.Registered) {
		fmt.Printf("room ready: %s:%d\n", r.PublicHost, r.PublicPort)
	}
	agent.OnRelayGone = func(relayURL string, err error) {
		newLogger().Warn("control connection lost", "relay", relayURL, "error", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return agent.Run(ctx)
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func printUsage() {
	fmt.Fprint(os.Stderr, `nonerp - reverse tunnel for Minecraft servers

Usage:
  nonerp <command> [flags]

Commands:
  server              run the relay server
  server token new    issue an access token and store it in the config
  server token ls     list issued access tokens
  client              run the host agent
  version             print version information
  help                print this message

Run "nonerp <command> -h" for command specific flags.
`)
}
