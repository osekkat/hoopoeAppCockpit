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
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	jobstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security"
)

type Config struct {
	Build               api.BuildInfo
	Events              *api.EventHub
	Jobs                jobstore.Reader
	Logger              api.Logger
	Redactor            api.Redactor
	WSValidator         api.WebSocketTokenValidator
	PublicBindConfirmer security.PublicBindConfirmer
	Now                 func() time.Time
	Stdout              io.Writer
	Stderr              io.Writer
}

func Run(ctx context.Context, args []string, cfg Config) error {
	flags := flag.NewFlagSet("hoopoed", flag.ContinueOnError)
	if cfg.Stderr != nil {
		flags.SetOutput(cfg.Stderr)
	}

	addr := flags.String("addr", "127.0.0.1:0", "daemon listen address")
	allowPublicBind := flags.Bool("allow-public-bind", false, "allow non-loopback listen addresses")
	publicBindToken := flags.String("public-bind-confirmation-token", "", "runtime confirmation token required with -allow-public-bind for public listen addresses")
	wsToken := flags.String("dev-ws-token", "", "development WebSocket token; empty accepts any token until auth wiring lands")
	if err := flags.Parse(args); err != nil {
		return err
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	decision, err := resolveListenDecision(ctx, listenDecisionRequest{
		Address:            *addr,
		ConfigAllowsPublic: *allowPublicBind,
		ConfirmationToken:  *publicBindToken,
		Confirmer:          cfg.PublicBindConfirmer,
		Logger:             cfg.Logger,
	})
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", decision.EffectiveAddress)
	if err != nil {
		return fmt.Errorf("listen %s: %w", decision.EffectiveAddress, err)
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
		Now:         now,
	})
	router = api.WithBindSafetyReport(router, security.NewBindReport(decision, now()))

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
	decision, err := resolveListenDecision(context.Background(), listenDecisionRequest{
		Address:            addr,
		ConfigAllowsPublic: allowPublicBind,
	})
	if err != nil {
		return err
	}
	if decision.PublicExposure {
		return fmt.Errorf("%w: %s", security.ErrPublicBindNotConfirmed, decision.RequestedAddress)
	}
	return nil
}

type listenDecisionRequest struct {
	Address            string
	ConfigAllowsPublic bool
	ConfirmationToken  string
	Confirmer          security.PublicBindConfirmer
	Logger             api.Logger
}

func resolveListenDecision(ctx context.Context, req listenDecisionRequest) (security.BindDecision, error) {
	decision, err := security.EvaluateBind(ctx, security.BindRequest{
		Address:            req.Address,
		ConfigAllowsPublic: req.ConfigAllowsPublic,
		ConfirmationToken:  req.ConfirmationToken,
		Confirmer:          req.Confirmer,
	})
	if err != nil {
		return security.BindDecision{}, err
	}
	logBindWarning(ctx, req.Logger, decision)
	return decision, nil
}

func logBindWarning(ctx context.Context, logger api.Logger, decision security.BindDecision) {
	if logger == nil || decision.Warning == nil {
		return
	}
	logger.Info(ctx, "security_public_bind_warning", map[string]any{
		"code":                 decision.Warning.Code,
		"severity":             decision.Warning.Severity,
		"message":              decision.Warning.Message,
		"requestedAddress":     decision.RequestedAddress,
		"effectiveAddress":     decision.EffectiveAddress,
		"configAllowsPublic":   decision.ConfigAllowsPublic,
		"runtimeConfirmed":     decision.RuntimeConfirmed,
		"publicExposure":       decision.PublicExposure,
		"tailnet":              decision.Tailnet,
		"loopback":             decision.Loopback,
		"diagnosticsWarningId": decision.Warning.DismissalID,
	})
}
