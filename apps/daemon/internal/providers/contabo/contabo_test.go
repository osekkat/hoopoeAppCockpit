package contabo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/logger"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestPluginManifestCatalogAndEstimate(t *testing.T) {
	t.Parallel()
	var _ schemas.ProviderPlugin = (*Plugin)(nil)

	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	plugin := New(Options{Now: func() time.Time { return now }})
	manifest := plugin.Manifest()
	if manifest.ProviderId != ProviderID || manifest.AuthMode != schemas.ApiToken {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, capability := range []schemas.ProviderPluginManifestCapabilities{
		schemas.VpsListRegions,
		schemas.VpsListSizes,
		schemas.VpsEstimateCost,
		schemas.VpsCreate,
		schemas.VpsDestroy,
	} {
		if !manifest.HasCapability(capability) {
			t.Fatalf("manifest missing capability %s", capability)
		}
	}
	regions, err := plugin.ListRegions(context.Background())
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	if len(regions) < 2 || regions[0].Id != "eu-central-1" {
		t.Fatalf("regions = %+v", regions)
	}
	sizes, err := plugin.ListSizes(context.Background(), "eu-central-1")
	if err != nil {
		t.Fatalf("ListSizes: %v", err)
	}
	if len(sizes) != 3 || sizes[2].Id != "cloud-vps-50" || sizes[2].Tier != schemas.Recommended {
		t.Fatalf("sizes = %+v", sizes)
	}
	estimate, err := plugin.EstimateMonthlyCost(context.Background(), schemas.ProviderEstimateCostOpts{
		Region: "eu-central-1",
		Size:   "cloud-vps-50",
	})
	if err != nil {
		t.Fatalf("EstimateMonthlyCost: %v", err)
	}
	if estimate.Usd != 56 || estimate.CatalogVersion != catalogVersion || !estimate.EstimatedAt.Equal(now) {
		t.Fatalf("estimate = %+v", estimate)
	}
	if sumCost(estimate.Breakdown) != estimate.Usd {
		t.Fatalf("breakdown sum %v != total %v", sumCost(estimate.Breakdown), estimate.Usd)
	}
	usEstimate, err := plugin.EstimateMonthlyCost(context.Background(), schemas.ProviderEstimateCostOpts{
		Region: "us-east-1",
		Size:   "cloud-vps-50",
	})
	if err != nil {
		t.Fatalf("EstimateMonthlyCost US: %v", err)
	}
	if usEstimate.Usd != 66 {
		t.Fatalf("US surcharge estimate = %v, want 66", usEstimate.Usd)
	}
}

func TestCreateInstanceCreatesSecretAndComputeInstance(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Now:        fixedNow,
	})

	instance, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "eu-central-1",
		Size:      "cloud-vps-50",
		Name:      "hoopoe-acfs-test",
		SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
		ImageId:   "ubuntu-24.04-image-id",
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if instance.InstanceId != "12345" || instance.Status != schemas.Running || instance.Size != "cloud-vps-50" {
		t.Fatalf("instance = %+v", instance)
	}
	if got := fake.count("POST /secrets"); got != 1 {
		t.Fatalf("POST /secrets count = %d, want 1", got)
	}
	if got := fake.count("POST /compute/instances"); got != 1 {
		t.Fatalf("POST /compute/instances count = %d, want 1", got)
	}
	if !strings.Contains(fake.lastCreateBody, `"productId":"V103"`) || !strings.Contains(fake.lastCreateBody, `"region":"EU"`) {
		t.Fatalf("create body = %s", fake.lastCreateBody)
	}
}

func TestCreateInstanceReturnsExistingByName(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	fake.existingDisplayName = "hoopoe-existing"
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Now:        fixedNow,
	})

	instance, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "eu-central-1",
		Size:      "cloud-vps-50",
		Name:      "hoopoe-existing",
		SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
	})
	if err != nil {
		t.Fatalf("CreateInstance existing: %v", err)
	}
	if instance.InstanceId != "777" {
		t.Fatalf("instance id = %s, want 777", instance.InstanceId)
	}
	if got := fake.count("POST /compute/instances"); got != 0 {
		t.Fatalf("create count = %d, want 0", got)
	}
}

func TestCreateInstanceCleansSSHSecretOnProvisionFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	fake.createStatus = http.StatusServiceUnavailable
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Now:        fixedNow,
	})

	_, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "eu-central-1",
		Size:      "cloud-vps-50",
		Name:      "hoopoe-fail",
		SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
	})
	var perr *ProviderError
	if !errors.As(err, &perr) || perr.ErrorClass != "provider_unavailable" {
		t.Fatalf("err = %v, want provider_unavailable ProviderError", err)
	}
	if got := fake.count("DELETE /secrets/321"); got != 1 {
		t.Fatalf("secret cleanup count = %d, want 1", got)
	}
}

func TestDestroyInstanceIsIdempotent(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Now:        fixedNow,
	})

	first, err := plugin.DestroyInstance(context.Background(), "12345")
	if err != nil {
		t.Fatalf("DestroyInstance first: %v", err)
	}
	if !first.Ok {
		t.Fatalf("first destroy = %+v", first)
	}
	fake.cancelStatus = http.StatusNotFound
	second, err := plugin.DestroyInstance(context.Background(), "12345")
	if err != nil {
		t.Fatalf("DestroyInstance second: %v", err)
	}
	if !second.Ok || second.InstanceId != "12345" {
		t.Fatalf("second destroy = %+v", second)
	}
}

func TestAPICallsEmitStructuredLogsWithoutSecrets(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	fake.expectedToken = "tok-secret-value"
	capture := logger.NewCaptureTransport(0)
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok-secret-value"),
		Logger: logger.New(logger.Config{
			Component: logger.ComponentDaemonAdapters,
			MinLevel:  logger.LevelDebug,
			Outputs:   []logger.Transport{capture},
			Now:       fixedNow,
		}),
		Now: fixedNow,
	})

	if _, err := plugin.DestroyInstance(context.Background(), "12345"); err != nil {
		t.Fatalf("DestroyInstance: %v", err)
	}
	entries := capture.Entries()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Level != logger.LevelInfo || entry.Component != logger.ComponentDaemonAdapters || entry.Subsystem != "providers.contabo" {
		t.Fatalf("entry envelope = %+v", entry)
	}
	if entry.Fields["plugin"] != ProviderID || entry.Fields["method"] != http.MethodPost ||
		entry.Fields["path"] != "/compute/instances/12345/cancel" || entry.Fields["statusCode"] != http.StatusCreated {
		t.Fatalf("entry fields = %+v", entry.Fields)
	}
	if strings.Contains(capture.JSONLines(), "tok-secret-value") {
		t.Fatalf("log leaked bearer token: %s", capture.JSONLines())
	}
}

func TestProviderErrorsEmitWarningLogs(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	fake.cancelStatus = http.StatusTooManyRequests
	capture := logger.NewCaptureTransport(0)
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Logger: logger.New(logger.Config{
			Component: logger.ComponentDaemonAdapters,
			MinLevel:  logger.LevelDebug,
			Outputs:   []logger.Transport{capture},
			Now:       fixedNow,
		}),
		Now: fixedNow,
	})

	_, err := plugin.DestroyInstance(context.Background(), "12345")
	var perr *ProviderError
	if !errors.As(err, &perr) || perr.ErrorClass != "rate_limited" {
		t.Fatalf("err = %v, want rate_limited ProviderError", err)
	}
	entries := capture.Entries()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1: %+v", len(entries), entries)
	}
	if entries[0].Level != logger.LevelWarn || entries[0].Fields["errorClass"] != "rate_limited" ||
		entries[0].Fields["statusCode"] != http.StatusTooManyRequests {
		t.Fatalf("warning entry = %+v", entries[0])
	}
}

func TestProviderErrorsCarryRetryHintsAndRedactedBody(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	fake.cancelStatus = http.StatusTooManyRequests
	fake.retryAfter = "90"
	fake.errorBody = `{"error":"Bearer sk-contabo-test-token-1234567890 used from /home/ubuntu/.ssh/id_ed25519"}`
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Now:        fixedNow,
	})

	_, err := plugin.DestroyInstance(context.Background(), "12345")
	var perr *ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %v, want ProviderError", err)
	}
	if perr.ErrorClass != "rate_limited" || !perr.Retryable {
		t.Fatalf("provider error = %+v, want retryable rate limit", perr)
	}
	if perr.RetryAfterSeconds == nil || *perr.RetryAfterSeconds != 90 {
		t.Fatalf("retryAfter = %+v, want 90", perr.RetryAfterSeconds)
	}
	if !strings.Contains(perr.SuggestedAction, "rate-limit") {
		t.Fatalf("suggested action = %q", perr.SuggestedAction)
	}
	if strings.Contains(perr.Body, "sk-contabo-test-token") || strings.Contains(perr.Body, "/home/ubuntu/.ssh") ||
		!strings.Contains(perr.Body, "[redacted") {
		t.Fatalf("body was not redacted: %q", perr.Body)
	}
}

func TestCreateAndDestroyEmitAuditEntries(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	auditSink := &recordingAudit{}
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Audit:      auditSink,
		Now:        fixedNow,
	})

	instance, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "eu-central-1",
		Size:      "cloud-vps-50",
		Name:      "hoopoe-acfs-test",
		SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
		ImageId:   "ubuntu-24.04-image-id",
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if _, err := plugin.DestroyInstance(context.Background(), instance.InstanceId); err != nil {
		t.Fatalf("DestroyInstance: %v", err)
	}
	if len(auditSink.entries) != 2 {
		t.Fatalf("audit entries = %+v", auditSink.entries)
	}
	create := auditSink.entries[0]
	if create.Action != "provider.contabo.create_instance" || create.Result != audit.ResultSuccess ||
		create.Actor.Kind != audit.ActorAdapter || create.Actor.ID != ProviderID {
		t.Fatalf("create audit = %+v", create)
	}
	if create.Data["instanceId"] != "12345" || create.Data["region"] != "eu-central-1" ||
		create.Data["size"] != "cloud-vps-50" {
		t.Fatalf("create audit data = %+v", create.Data)
	}
	if _, ok := create.Data["sshPubKey"]; ok {
		t.Fatalf("create audit leaked ssh key material: %+v", create.Data)
	}
	destroy := auditSink.entries[1]
	if destroy.Action != "provider.contabo.destroy_instance" || destroy.Result != audit.ResultSuccess ||
		destroy.Data["instanceId"] != "12345" || destroy.Data["ok"] != true {
		t.Fatalf("destroy audit = %+v", destroy)
	}
}

func TestSuccessfulMutationSurfacesAuditFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	auditSink := &recordingAudit{err: errors.New("audit unavailable")}
	plugin := New(Options{
		BaseURL:    fake.server.URL,
		HTTPClient: fake.server.Client(),
		Token:      staticToken("tok"),
		Audit:      auditSink,
		Now:        fixedNow,
	})

	result, err := plugin.DestroyInstance(context.Background(), "12345")
	if err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("err = %v, want audit failure", err)
	}
	if result == nil || !result.Ok {
		t.Fatalf("result = %+v, want provider result preserved", result)
	}
}

func TestProviderErrorsAreClassified(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status    int
		class     string
		retryable bool
	}{
		{http.StatusUnauthorized, "auth", false},
		{http.StatusTooManyRequests, "rate_limited", true},
		{http.StatusServiceUnavailable, "provider_unavailable", true},
	} {
		t.Run(tc.class, func(t *testing.T) {
			t.Parallel()
			fake := newFakeAPI(t)
			fake.listStatus = tc.status
			plugin := New(Options{
				BaseURL:    fake.server.URL,
				HTTPClient: fake.server.Client(),
				Token:      staticToken("tok"),
				Now:        fixedNow,
			})
			_, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
				Region:    "eu-central-1",
				Size:      "cloud-vps-50",
				Name:      "hoopoe-error",
				SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
			})
			var perr *ProviderError
			if !errors.As(err, &perr) || perr.ErrorClass != tc.class || perr.Retryable != tc.retryable {
				t.Fatalf("err = %v, want class %s retryable=%v", err, tc.class, tc.retryable)
			}
		})
	}
}

func TestValidationRejectsBadInputs(t *testing.T) {
	t.Parallel()
	plugin := New(Options{})
	if _, err := plugin.ListSizes(context.Background(), "moon"); !errors.Is(err, ErrUnknownRegion) {
		t.Fatalf("ListSizes err = %v, want ErrUnknownRegion", err)
	}
	if _, err := plugin.EstimateMonthlyCost(context.Background(), schemas.ProviderEstimateCostOpts{Region: "eu-central-1", Size: "tiny"}); !errors.Is(err, ErrUnknownSize) {
		t.Fatalf("Estimate err = %v, want ErrUnknownSize", err)
	}
	_, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "eu-central-1",
		Size:      "cloud-vps-50",
		Name:      "bad-key",
		SshPubKey: "not a key",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Create err = %v, want ErrInvalidRequest", err)
	}
}

type fakeAPI struct {
	t                   *testing.T
	server              *httptest.Server
	mu                  sync.Mutex
	counts              map[string]int
	existingDisplayName string
	listStatus          int
	createStatus        int
	cancelStatus        int
	retryAfter          string
	errorBody           string
	lastCreateBody      string
	expectedToken       string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{
		t:             t,
		counts:        map[string]int{},
		listStatus:    http.StatusOK,
		createStatus:  http.StatusCreated,
		cancelStatus:  http.StatusCreated,
		expectedToken: "tok",
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.counts[r.Method+" "+r.URL.Path]++
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+f.expectedToken {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("x-request-id") == "" {
		http.Error(w, "missing request id", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/compute/instances":
		if f.listStatus != http.StatusOK {
			f.writeProviderFailure(w, "list failed", f.listStatus)
			return
		}
		if f.existingDisplayName != "" {
			writeJSON(w, http.StatusOK, contaboResponse[contaboInstance]{Data: []contaboInstance{fakeInstance(777, f.existingDisplayName)}})
			return
		}
		writeJSON(w, http.StatusOK, contaboResponse[contaboInstance]{Data: []contaboInstance{}})
	case r.Method == http.MethodPost && r.URL.Path == "/secrets":
		writeJSON(w, http.StatusCreated, contaboResponse[contaboSecret]{Data: []contaboSecret{{SecretID: "321"}}})
	case r.Method == http.MethodDelete && r.URL.Path == "/secrets/321":
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/compute/instances":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		encoded, _ := json.Marshal(body)
		f.mu.Lock()
		f.lastCreateBody = string(encoded)
		f.mu.Unlock()
		if f.createStatus != http.StatusCreated {
			f.writeProviderFailure(w, "create failed", f.createStatus)
			return
		}
		writeJSON(w, http.StatusCreated, contaboResponse[contaboInstance]{Data: []contaboInstance{fakeInstance(12345, "hoopoe-acfs-test")}})
	case r.Method == http.MethodPost && r.URL.Path == "/compute/instances/12345/cancel":
		if f.cancelStatus != http.StatusCreated {
			f.writeProviderFailure(w, "cancel failed", f.cancelStatus)
			return
		}
		writeJSON(w, http.StatusCreated, contaboResponse[contaboCancelResult]{Data: []contaboCancelResult{{CancelDate: "2026-05-04"}}})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) writeProviderFailure(w http.ResponseWriter, fallback string, status int) {
	if f.retryAfter != "" {
		w.Header().Set("Retry-After", f.retryAfter)
	}
	if f.errorBody != "" {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(f.errorBody))
		return
	}
	http.Error(w, fallback, status)
}

func (f *fakeAPI) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[key]
}

func fakeInstance(id int64, name string) contaboInstance {
	inst := contaboInstance{
		InstanceID:  id,
		DisplayName: name,
		ProductID:   "V103",
		Region:      "EU",
		Status:      "running",
		CreatedAt:   time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC),
	}
	inst.IPConfig.V4 = &struct {
		IP string `json:"ip"`
	}{IP: "203.0.113.10"}
	return inst
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func staticToken(token string) TokenSource {
	return TokenSourceFunc(func(context.Context) (string, error) {
		return token, nil
	})
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

type recordingAudit struct {
	entries []audit.Entry
	err     error
}

func (r *recordingAudit) Append(entry audit.Entry) (audit.Entry, []redaction.TraceEvent, error) {
	r.entries = append(r.entries, entry)
	return entry, nil, r.err
}
