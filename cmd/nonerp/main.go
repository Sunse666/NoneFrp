// Command nonerp is the single binary used both as the relay server and as
// the host-side agent.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
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
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "server.yaml", "path to the server configuration file")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return fmt.Errorf("server mode is not implemented yet (config %q)", *configPath)
}

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	configPath := fs.String("config", "client.yaml", "path to the client configuration file")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return fmt.Errorf("client mode is not implemented yet (config %q)", *configPath)
}

func printUsage() {
	fmt.Fprint(os.Stderr, `nonerp - reverse tunnel for Minecraft servers

Usage:
  nonerp <command> [flags]

Commands:
  server            run the relay server
  client            run the host agent
  version           print version information
  help              print this message

Run "nonerp <command> -h" for command specific flags.
`)
}
