// Package main hosts the hoopoed daemon entry point.
package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/mock"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/transport"
)

var (
	version    = "0.0.0"
	commit     = "dev"
	buildDate  = "dev"
	apiVersion = "v1"
)

func main() {
	if wantsMockMode(os.Args[1:]) {
		if err := mock.Run(context.Background(), os.Args[1:], mock.ServerConfig{
			Build: api.BuildInfo{
				Version:    version,
				Commit:     commit,
				BuildDate:  buildDate,
				APIVersion: apiVersion,
			},
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}); err != nil {
			log.Printf("hoopoed mock failed: %v", err)
			os.Exit(1)
		}
		return
	}
	if err := transport.Run(context.Background(), os.Args[1:], transport.Config{
		Build: api.BuildInfo{
			Version:    version,
			Commit:     commit,
			BuildDate:  buildDate,
			APIVersion: apiVersion,
		},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}); err != nil {
		log.Printf("hoopoed failed: %v", err)
		os.Exit(1)
	}
}

func wantsMockMode(args []string) bool {
	if os.Getenv("HOOPOE_MOCK_SCENARIO") != "" {
		return true
	}
	for _, arg := range args {
		if arg == "--mock" || arg == "-mock" {
			return true
		}
		if strings.HasPrefix(arg, "--mock=") {
			return strings.TrimPrefix(arg, "--mock=") != "false"
		}
		if strings.HasPrefix(arg, "-mock=") {
			return strings.TrimPrefix(arg, "-mock=") != "false"
		}
	}
	return false
}
