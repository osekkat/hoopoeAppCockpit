// Package transport owns the daemon's HTTP listener bootstrap.
package transport

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	jobstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

type Config struct {
	Build       api.BuildInfo
	Events      *api.EventHub
	Jobs        jobstore.Reader
	Logger      api.Logger
	Redactor    api.Redactor
	WSValidator api.WebSocketTokenValidator
	Stdout      io.Writer
	Stderr      io.Writer
}

func Run(ctx context.Context, args []string, cfg Config) error {
	flags := flag.NewFlagSet("hoopoed", flag.ContinueOnError)
	if cfg.Stderr != nil {
		flags.SetOutput(cfg.Stderr)
	}

	addr := flags.String("addr", "127.0.0.1:0", "daemon listen address")
	allowPublicBind := flags.Bool("allow-public-bind", false, "allow non-loopback listen addresses")
	wsToken := flags.String("dev-ws-token", "", "development WebSocket token; empty accepts any token until auth wiring lands")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := ValidateListenAddress(*addr, *allowPublicBind); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", *addr, err)
	}
	defer listener.Close()

	wsValidator := cfg.WSValidator
	if wsValidator == nil {
		wsValidator = api.StaticWebSocketTokenValidator{Token: *wsToken}
	}
	router := api.NewRouter(api.Config{
		Build:       cfg.Build,
		Events:      cfg.Events,
		Jobs:        cfg.Jobs,
		Logger:      cfg.Logger,
		Redactor:    cfg.Redactor,
		WSValidator: wsValidator,
	})

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
		Handler:           router,
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

func ValidateListenAddress(addr string, allowPublicBind bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	if allowPublicBind {
		return nil
	}
	if host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1" {
		return nil
	}
	return fmt.Errorf("refusing public bind %q without -allow-public-bind", addr)
}
