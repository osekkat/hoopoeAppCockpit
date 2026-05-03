package mock

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/transport"
)

type ServerConfig struct {
	Build  api.BuildInfo
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

func Run(ctx context.Context, args []string, cfg ServerConfig) error {
	flags := flag.NewFlagSet("hoopoed-mock", flag.ContinueOnError)
	if cfg.Stderr != nil {
		flags.SetOutput(cfg.Stderr)
	}

	defaultScenario := firstEnv("HOOPOE_MOCK_SCENARIO", "fresh")
	defaultRoot := os.Getenv("HOOPOE_MOCK_FIXTURE_ROOT")

	addr := flags.String("addr", "127.0.0.1:0", "mock daemon listen address")
	allowPublicBind := flags.Bool("allow-public-bind", false, "allow non-loopback listen addresses")
	mockMode := flags.Bool("mock", true, "boot from fixture-backed Mock Flywheel Mode")
	scenarioName := flags.String("scenario", defaultScenario, "mock fixture scenario")
	fixtureRoot := flags.String("fixture-root", defaultRoot, "path to phase0 scenario root")
	speed := flags.Int("speed", parseSpeedEnv(), "event replay acceleration multiplier")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*mockMode {
		return errors.New("hoopoed-mock only supports -mock=true")
	}
	if *speed <= 0 {
		return fmt.Errorf("speed must be >= 1")
	}
	if err := transport.ValidateListenAddress(*addr, *allowPublicBind); err != nil {
		return err
	}

	daemon, err := NewDaemon(Config{
		Scenario:    *scenarioName,
		FixtureRoot: *fixtureRoot,
		Build:       cfg.Build,
		Now:         cfg.Now,
	})
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", *addr, err)
	}
	defer listener.Close()

	stdout := cfg.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		fmt.Fprintf(stdout, "listening on %d\n", tcpAddr.Port)
	} else {
		fmt.Fprintf(stdout, "listening on %s\n", listener.Addr().String())
	}

	server := &http.Server{
		Handler:           daemon.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func parseSpeedEnv() int {
	raw := os.Getenv("HOOPOE_MOCK_SPEED")
	if raw == "" {
		return 1
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 1
	}
	return value
}

func firstEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
