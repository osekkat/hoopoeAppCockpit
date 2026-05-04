package ovh

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
	if len(regions) < 2 || regions[0].Id != "bhs" {
		t.Fatalf("regions = %+v", regions)
	}
	sizes, err := plugin.ListSizes(context.Background(), "bhs")
	if err != nil {
		t.Fatalf("ListSizes: %v", err)
	}
	if len(sizes) != 3 || sizes[2].Id != "vps-5" || sizes[2].Tier != schemas.Recommended {
		t.Fatalf("sizes = %+v", sizes)
	}
	estimate, err := plugin.EstimateMonthlyCost(context.Background(), schemas.ProviderEstimateCostOpts{
		Region: "bhs",
		Size:   "vps-5",
	})
	if err != nil {
		t.Fatalf("EstimateMonthlyCost: %v", err)
	}
	if estimate.Usd != 40 || estimate.CatalogVersion != catalogVersion || !estimate.EstimatedAt.Equal(now) {
		t.Fatalf("estimate = %+v", estimate)
	}
	if sumCost(estimate.Breakdown) != estimate.Usd {
		t.Fatalf("breakdown sum %v != total %v", sumCost(estimate.Breakdown), estimate.Usd)
	}
}

func TestCreateInstanceCreatesSSHKeyAndComputeInstance(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	plugin := New(Options{
		BaseURL:     fake.server.URL,
		ProjectID:   "proj-1",
		HTTPClient:  fake.server.Client(),
		Credentials: staticCredentials(),
		Now:         fixedNow,
	})

	instance, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "bhs",
		Size:      "vps-5",
		Name:      "hoopoe-acfs-test",
		SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
		ImageId:   "ubuntu-24.04-image-id",
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if instance.InstanceId != "inst-123" || instance.Status != schemas.Running || instance.Size != "vps-5" {
		t.Fatalf("instance = %+v", instance)
	}
	if got := fake.count("POST /cloud/project/proj-1/sshkey"); got != 1 {
		t.Fatalf("POST sshkey count = %d, want 1", got)
	}
	if got := fake.count("POST /cloud/project/proj-1/instance"); got != 1 {
		t.Fatalf("POST instance count = %d, want 1", got)
	}
	if !strings.Contains(fake.lastCreateBody, `"flavorId":"vps-5"`) || !strings.Contains(fake.lastCreateBody, `"region":"BHS5"`) {
		t.Fatalf("create body = %s", fake.lastCreateBody)
	}
}

func TestCreateInstanceReturnsExistingByName(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	fake.existingName = "hoopoe-existing"
	plugin := New(Options{
		BaseURL:     fake.server.URL,
		ProjectID:   "proj-1",
		HTTPClient:  fake.server.Client(),
		Credentials: staticCredentials(),
		Now:         fixedNow,
	})

	instance, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "bhs",
		Size:      "vps-5",
		Name:      "hoopoe-existing",
		SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
	})
	if err != nil {
		t.Fatalf("CreateInstance existing: %v", err)
	}
	if instance.InstanceId != "inst-existing" {
		t.Fatalf("instance id = %s, want inst-existing", instance.InstanceId)
	}
	if got := fake.count("POST /cloud/project/proj-1/instance"); got != 0 {
		t.Fatalf("create count = %d, want 0", got)
	}
}

func TestCreateInstanceCleansSSHKeyOnProvisionFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	fake.createStatus = http.StatusServiceUnavailable
	plugin := New(Options{
		BaseURL:     fake.server.URL,
		ProjectID:   "proj-1",
		HTTPClient:  fake.server.Client(),
		Credentials: staticCredentials(),
		Now:         fixedNow,
	})

	_, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "bhs",
		Size:      "vps-5",
		Name:      "hoopoe-fail",
		SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
	})
	var perr *ProviderError
	if !errors.As(err, &perr) || perr.ErrorClass != "provider_unavailable" {
		t.Fatalf("err = %v, want provider_unavailable ProviderError", err)
	}
	if got := fake.count("DELETE /cloud/project/proj-1/sshkey/key-321"); got != 1 {
		t.Fatalf("ssh key cleanup count = %d, want 1", got)
	}
}

func TestDestroyInstanceIsIdempotent(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	plugin := New(Options{
		BaseURL:     fake.server.URL,
		ProjectID:   "proj-1",
		HTTPClient:  fake.server.Client(),
		Credentials: staticCredentials(),
		Now:         fixedNow,
	})

	first, err := plugin.DestroyInstance(context.Background(), "inst-123")
	if err != nil {
		t.Fatalf("DestroyInstance first: %v", err)
	}
	if !first.Ok {
		t.Fatalf("first destroy = %+v", first)
	}
	fake.destroyStatus = http.StatusNotFound
	second, err := plugin.DestroyInstance(context.Background(), "inst-123")
	if err != nil {
		t.Fatalf("DestroyInstance second: %v", err)
	}
	if !second.Ok || second.InstanceId != "inst-123" {
		t.Fatalf("second destroy = %+v", second)
	}
}

func TestProviderErrorsAreClassified(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		class  string
	}{
		{http.StatusUnauthorized, "auth"},
		{http.StatusTooManyRequests, "rate_limited"},
		{http.StatusServiceUnavailable, "provider_unavailable"},
	} {
		t.Run(tc.class, func(t *testing.T) {
			t.Parallel()
			fake := newFakeAPI(t)
			fake.listStatus = tc.status
			plugin := New(Options{
				BaseURL:     fake.server.URL,
				ProjectID:   "proj-1",
				HTTPClient:  fake.server.Client(),
				Credentials: staticCredentials(),
				Now:         fixedNow,
			})
			_, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
				Region:    "bhs",
				Size:      "vps-5",
				Name:      "hoopoe-error",
				SshPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest user@host",
			})
			var perr *ProviderError
			if !errors.As(err, &perr) || perr.ErrorClass != tc.class {
				t.Fatalf("err = %v, want class %s", err, tc.class)
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
	if _, err := plugin.EstimateMonthlyCost(context.Background(), schemas.ProviderEstimateCostOpts{Region: "bhs", Size: "tiny"}); !errors.Is(err, ErrUnknownSize) {
		t.Fatalf("Estimate err = %v, want ErrUnknownSize", err)
	}
	_, err := plugin.CreateInstance(context.Background(), schemas.ProviderCreateInstanceOpts{
		Region:    "bhs",
		Size:      "vps-5",
		Name:      "bad-key",
		SshPubKey: "not a key",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Create err = %v, want ErrInvalidRequest", err)
	}
}

func TestProjectAndCredentialsAreRequiredForLiveCalls(t *testing.T) {
	t.Parallel()
	plugin := New(Options{Credentials: staticCredentials()})
	_, err := plugin.DestroyInstance(context.Background(), "inst-123")
	if !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("Destroy err = %v, want ErrProjectRequired", err)
	}
	plugin = New(Options{ProjectID: "proj-1"})
	_, err = plugin.DestroyInstance(context.Background(), "inst-123")
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Destroy err = %v, want ErrAuthRequired", err)
	}
}

type fakeAPI struct {
	t              *testing.T
	server         *httptest.Server
	mu             sync.Mutex
	counts         map[string]int
	existingName   string
	listStatus     int
	createStatus   int
	destroyStatus  int
	lastCreateBody string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{
		t:             t,
		counts:        map[string]int{},
		listStatus:    http.StatusOK,
		createStatus:  http.StatusCreated,
		destroyStatus: http.StatusNoContent,
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.counts[r.Method+" "+r.URL.Path]++
	f.mu.Unlock()
	if r.Header.Get("X-Ovh-Application") != "app-key" || r.Header.Get("X-Ovh-Consumer") != "consumer-key" {
		http.Error(w, "missing OVH credentials", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("X-Ovh-Timestamp") != "1777896000" || !strings.HasPrefix(r.Header.Get("X-Ovh-Signature"), "$1$") {
		http.Error(w, "missing OVH signature", http.StatusBadRequest)
		return
	}
	if r.Header.Get("x-request-id") == "" {
		http.Error(w, "missing request id", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/cloud/project/proj-1/instance":
		if f.listStatus != http.StatusOK {
			http.Error(w, "list failed", f.listStatus)
			return
		}
		if f.existingName != "" {
			writeJSON(w, http.StatusOK, []ovhInstance{fakeInstance("inst-existing", f.existingName)})
			return
		}
		writeJSON(w, http.StatusOK, []ovhInstance{})
	case r.Method == http.MethodGet && r.URL.Path == "/cloud/project/proj-1/sshkey":
		writeJSON(w, http.StatusOK, []ovhSSHKey{})
	case r.Method == http.MethodPost && r.URL.Path == "/cloud/project/proj-1/sshkey":
		writeJSON(w, http.StatusCreated, ovhSSHKey{ID: "key-321", Name: "hoopoe-test"})
	case r.Method == http.MethodDelete && r.URL.Path == "/cloud/project/proj-1/sshkey/key-321":
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/cloud/project/proj-1/instance":
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
			http.Error(w, "create failed", f.createStatus)
			return
		}
		writeJSON(w, http.StatusCreated, fakeInstance("inst-123", "hoopoe-acfs-test"))
	case r.Method == http.MethodDelete && r.URL.Path == "/cloud/project/proj-1/instance/inst-123":
		if f.destroyStatus != http.StatusNoContent {
			http.Error(w, "destroy failed", f.destroyStatus)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[key]
}

func fakeInstance(id, name string) ovhInstance {
	return ovhInstance{
		ID:        id,
		Name:      name,
		FlavorID:  "vps-5",
		Region:    "BHS5",
		Status:    "ACTIVE",
		CreatedAt: time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC),
		IPAddresses: []ovhIPAddress{
			{IP: "203.0.113.20", Version: 4},
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func staticCredentials() CredentialSource {
	return CredentialSourceFunc(func(context.Context) (Credentials, error) {
		return Credentials{
			ApplicationKey:    "app-key",
			ApplicationSecret: "app-secret",
			ConsumerKey:       "consumer-key",
		}, nil
	})
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}
