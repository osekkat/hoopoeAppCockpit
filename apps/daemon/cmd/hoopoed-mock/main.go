// Package main hosts the fixture-backed mock daemon entry point.
package main

import (
	"context"
	"log"
	"os"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/mock"
)

var (
	version    = "0.0.0"
	commit     = "mock"
	buildDate  = "mock"
	apiVersion = "v1"
)

func main() {
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
		log.Printf("hoopoed-mock failed: %v", err)
		os.Exit(1)
	}
}
