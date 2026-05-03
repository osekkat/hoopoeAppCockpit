// Package main hosts the hoopoed daemon entry point.
package main

import (
	"context"
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
