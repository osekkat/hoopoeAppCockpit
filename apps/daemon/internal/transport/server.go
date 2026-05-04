// Package transport owns the daemon's HTTP listener bootstrap.
package transport

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/approvals"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	jobstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	daemonmetrics "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/metrics"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/onboarding/checkpoints"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/systemd"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/telemetry"
	_ "modernc.org/sqlite"
)

type Config struct {
	Build               api.BuildInfo
	Events              *api.EventHub
	Jobs                jobstore.Reader
	Auth                *api.AuthConfig
	Onboarding          *checkpoints.Service
	Logger              api.Logger
	Redactor            api.Redactor
	WSValidator         api.WebSocketTokenValidator
	Capabilities        *capabilities.Registry
	Inventory           api.InventoryService
	Metrics             *daemonmetrics.Registry
	Telemetry           *telemetry.Service
	PublicBindConfirmer security.PublicBindConfirmer
	StateDir            string
	SystemdNotifier     systemdNotifier
	Now                 func() time.Time
	Stdout              io.Writer
	Stderr              io.Writer
}

type systemdNotifier interface {
	Ready(ctx context.Context, status string) error
	Watchdog(ctx context.Context) error
	WatchdogInterval() (time.Duration, bool, error)
}

func Run(ctx context.Context, args []string, cfg Config) error {
	flags := flag.NewFlagSet("hoopoed", flag.ContinueOnError)
	if cfg.Stderr != nil {
		flags.SetOutput(cfg.Stderr)
	}

	addr := flags.String("addr", "127.0.0.1:0", "daemon listen address")
	stateDir := flags.String("state-dir", cfg.StateDir, "daemon state directory; defaults to $HOOPOE_HOME or ~/.hoopoe")
	bootstrapTokenOnly := flags.Bool("bootstrap-token-only", false, "initialize daemon auth state, print the first pairing token when created, and exit")
	allowPublicBind := flags.Bool("allow-public-bind", false, "allow non-loopback listen addresses")
	publicBindToken := flags.String("public-bind-confirmation-token", "", "runtime confirmation token required with -allow-public-bind for public listen addresses")
	wsToken := flags.String("dev-ws-token", "", "development WebSocket token; empty accepts any token until auth wiring lands")
	telemetryOptIn := flags.Bool("telemetry-opt-in", false, "enable local crash report and aggregate telemetry recording")
	telemetryCollectorURL := flags.String("telemetry-collector-url", "", "reserved collector URL; daemon records locally and never uploads directly")
	if err := flags.Parse(args); err != nil {
		return err
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	resolvedStateDir := resolveStateDir(*stateDir)
	authRuntime, err := prepareAuthRuntime(ctx, resolvedStateDir, now, cfg.Auth)
	if err != nil {
		return err
	}
	if *bootstrapTokenOnly {
		writeInitialPairing(stdout, authRuntime.initialPairing, authRuntime.initialPairingCreated)
		if !authRuntime.initialPairingCreated {
			fmt.Fprintln(stdout, "HOOPOE_PAIRING_TOKEN_ALREADY_INITIALIZED=1")
		}
		return nil
	}
	onboarding, closeOnboarding, err := prepareOnboardingRuntime(ctx, resolvedStateDir, now, cfg.Onboarding)
	if err != nil {
		return err
	}
	defer closeOnboarding()
	telemetryService, err := prepareTelemetryRuntime(resolvedStateDir, now, cfg.Telemetry, *telemetryOptIn, *telemetryCollectorURL)
	if err != nil {
		return err
	}
	auditWriter, err := audit.NewWriter(audit.Config{
		Path: filepath.Join(resolvedStateDir, "audit.jsonl"),
		Now:  now,
	})
	if err != nil {
		return err
	}
	defer auditWriter.Close()

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

	wsValidator := resolveWSValidator(cfg.WSValidator, authRuntime.wsValidator, *wsToken)
	if wsValidator == nil {
		wsValidator = api.StaticWebSocketTokenValidator{Token: *wsToken}
	}
	router := api.NewRouter(api.Config{
		Build:        cfg.Build,
		Events:       cfg.Events,
		Jobs:         cfg.Jobs,
		Auth:         authRuntime.config,
		Onboarding:   onboarding,
		Audit:        auditWriter,
		Logger:       cfg.Logger,
		Redactor:     cfg.Redactor,
		WSValidator:  wsValidator,
		Capabilities: cfg.Capabilities,
		Inventory:    cfg.Inventory,
		Metrics:      cfg.Metrics,
		Telemetry:    telemetryService,
		Now:          now,
	})
	router = api.WithBindSafetyReport(router, security.NewBindReport(decision, now()))

	writeInitialPairing(stdout, authRuntime.initialPairing, authRuntime.initialPairingCreated)
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		fmt.Fprintf(stdout, "listening on %d\n", tcpAddr.Port)
	} else {
		fmt.Fprintf(stdout, "listening on %s\n", listener.Addr().String())
	}

	notifier := cfg.SystemdNotifier
	if notifier == nil {
		defaultNotifier := systemd.NewNotifier()
		notifier = defaultNotifier
	}
	if err := notifier.Ready(ctx, "hoopoe daemon accepting requests"); err != nil {
		return err
	}
	if err := startWatchdog(ctx, notifier, cfg.Logger); err != nil {
		return err
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

func prepareTelemetryRuntime(stateDir string, now func() time.Time, configured *telemetry.Service, optIn bool, collectorURL string) (*telemetry.Service, error) {
	if configured != nil {
		return configured, nil
	}
	return telemetry.NewService(telemetry.Config{
		Enabled:      optIn,
		Path:         telemetry.DefaultPath(stateDir),
		CollectorURL: collectorURL,
		Now:          now,
	})
}

func prepareOnboardingRuntime(ctx context.Context, stateDir string, now func() time.Time, configured *checkpoints.Service) (*checkpoints.Service, func() error, error) {
	if configured != nil {
		return configured, func() error { return nil }, nil
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("onboarding state dir: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(stateDir, "onboarding.sqlite3"))
	if err != nil {
		return nil, nil, fmt.Errorf("onboarding sqlite open: %w", err)
	}
	store, err := checkpoints.NewSQLStore(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	service := checkpoints.NewService(checkpoints.Config{Store: store, Now: now})
	return service, db.Close, nil
}

type authRuntime struct {
	config                *api.AuthConfig
	wsValidator           api.WebSocketTokenValidator
	initialPairing        auth.IssuedPairing
	initialPairingCreated bool
}

func prepareAuthRuntime(ctx context.Context, stateDir string, now func() time.Time, configured *api.AuthConfig) (authRuntime, error) {
	if configured != nil {
		return authRuntime{config: configured}, nil
	}
	authDir := filepath.Join(stateDir, "auth")
	pairings, err := auth.NewBootstrapCredentialService(auth.BootstrapCredentialConfig{
		Path: filepath.Join(authDir, "pairings.jsonl"),
		Now:  now,
	})
	if err != nil {
		return authRuntime{}, err
	}
	secrets, err := auth.NewServerSecretStore(auth.ServerSecretStoreConfig{
		Path: filepath.Join(authDir, "server-secret.json"),
		Now:  now,
	})
	if err != nil {
		return authRuntime{}, err
	}
	if _, err := secrets.EnsureInitialized(); err != nil {
		return authRuntime{}, err
	}
	sessions, err := auth.NewSessionCredentialService(auth.SessionCredentialConfig{
		Secrets: secrets,
		Now:     now,
	})
	if err != nil {
		return authRuntime{}, err
	}
	initial, created, err := pairings.EnsureInitialPairing(ctx)
	if err != nil {
		return authRuntime{}, err
	}
	return authRuntime{
		config: &api.AuthConfig{
			Service:   sessions,
			Pairing:   pairings,
			Approvals: api.ApprovalQueueLookup{Queue: approvals.NewQueue(approvals.Config{Now: now})},
		},
		wsValidator:           sessionWebSocketValidator{service: sessions},
		initialPairing:        initial,
		initialPairingCreated: created,
	}, nil
}

func resolveStateDir(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		candidate = strings.TrimSpace(os.Getenv("HOOPOE_HOME"))
	}
	if candidate == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			candidate = filepath.Join(home, ".hoopoe")
		}
	}
	if candidate == "" {
		candidate = ".hoopoe"
	}
	return filepath.Clean(candidate)
}

func writeInitialPairing(stdout io.Writer, issued auth.IssuedPairing, created bool) {
	if !created {
		return
	}
	fmt.Fprintf(stdout, "HOOPOE_PAIRING_TOKEN=%s\n", issued.DisplayToken)
	fmt.Fprintf(stdout, "HOOPOE_PAIRING_TOKEN_ID=%s\n", issued.TokenID)
}

func resolveWSValidator(configured api.WebSocketTokenValidator, session api.WebSocketTokenValidator, devToken string) api.WebSocketTokenValidator {
	if configured != nil {
		return configured
	}
	if devToken != "" {
		return api.StaticWebSocketTokenValidator{Token: devToken}
	}
	return session
}

type sessionWebSocketValidator struct {
	service interface {
		VerifyWSToken(token string) (auth.Claims, error)
	}
}

func (v sessionWebSocketValidator) ValidateWebSocketToken(_ context.Context, token string) error {
	_, err := v.service.VerifyWSToken(token)
	return err
}

func startWatchdog(ctx context.Context, notifier systemdNotifier, logger api.Logger) error {
	interval, active, err := notifier.WatchdogInterval()
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := notifier.Watchdog(ctx); err != nil {
					if logger != nil {
						logger.Error(ctx, "systemd_watchdog_notify_failed", map[string]any{"error": err.Error()})
					}
					return
				}
			}
		}
	}()
	return nil
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
