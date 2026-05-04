// Package main hosts the Hoopoe Go daemon entry point + the operator
// CLI subcommands. Subcommand dispatch:
//
//   hoopoe                  → starts the daemon (default; preserves the
//                              pre-hp-uz6 invocation shape)
//   hoopoe serve [...]      → explicit form for the daemon (same as no-args)
//   hoopoe auth ...         → hp-uz6 operator CLI
//                             (pairing/session create|list|revoke,
//                              rotate-secret)
//   hoopoe --help|-h        → usage
//
// When no subcommand is given, behavior is unchanged from before
// hp-uz6: the binary boots the daemon. Wrapping CLI subcommands in
// the same binary mirrors t3code's `t3` and avoids shipping a
// separate `hoopoectl` binary.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/transport"
)

var (
	version    = "0.0.0"
	commit     = "dev"
	buildDate  = "dev"
	apiVersion = "v1"
)

func main() {
	args := os.Args[1:]
	if err := dispatch(context.Background(), args); err != nil {
		log.Printf("hoopoe: %v", err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 || isFlagFirst(args[0]) {
		return runServe(ctx, args)
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:])
	case "auth":
		return runAuth(ctx, args[1:], &authIO{
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
		})
	case "--help", "-h", "help":
		printRootUsage(os.Stdout)
		return nil
	default:
		printRootUsage(os.Stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runServe(ctx context.Context, args []string) error {
	return transport.Run(ctx, args, transport.Config{
		Build: api.BuildInfo{
			Version:    version,
			Commit:     commit,
			BuildDate:  buildDate,
			APIVersion: apiVersion,
		},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

func isFlagFirst(arg string) bool {
	return len(arg) > 0 && arg[0] == '-'
}

func printRootUsage(w *os.File) {
	fmt.Fprint(w, `usage: hoopoe [subcommand] [args]

subcommands:
  serve [flags]   start the HTTP daemon (default when no subcommand given)
  auth ...        hp-uz6 operator CLI (pairing / session / rotate-secret)

run 'hoopoe auth --help' for the auth subcommand surface.
`)
}
