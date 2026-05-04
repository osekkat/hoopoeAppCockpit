// Package ovh implements Hoopoe's built-in OVHcloud VPS provider plugin.
package ovh

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	ProviderID     = "ovh"
	defaultBaseURL = "https://api.ovh.com/1.0"
	defaultImageID = "ubuntu-24.04"
	catalogVersion = "ovh-2026-05-04"
)

var (
	ErrInvalidRequest      = errors.New("ovh: invalid request")
	ErrAuthRequired        = errors.New("ovh: signed API credential source required")
	ErrProjectRequired     = errors.New("ovh: public cloud project id required")
	ErrUnknownRegion       = errors.New("ovh: unknown region")
	ErrUnknownSize         = errors.New("ovh: unknown size")
	ErrProviderUnavailable = errors.New("ovh: provider unavailable")
)

type Credentials struct {
	ApplicationKey    string
	ApplicationSecret string
	ConsumerKey       string
}

type CredentialSource interface {
	Credentials(ctx context.Context) (Credentials, error)
}

type CredentialSourceFunc func(ctx context.Context) (Credentials, error)

func (f CredentialSourceFunc) Credentials(ctx context.Context) (Credentials, error) {
	return f(ctx)
}

type Options struct {
	BaseURL         string
	ProjectID       string
	PublicNetworkID string
	HTTPClient      *http.Client
	Credentials     CredentialSource
	Now             func() time.Time
}

type Plugin struct {
	baseURL         string
	projectID       string
	publicNetworkID string
	client          *http.Client
	credentials     CredentialSource
	now             func() time.Time
}

var _ schemas.ProviderPlugin = (*Plugin)(nil)

func New(opts Options) *Plugin {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Plugin{
		baseURL:         baseURL,
		projectID:       strings.TrimSpace(opts.ProjectID),
		publicNetworkID: strings.TrimSpace(opts.PublicNetworkID),
		client:          client,
		credentials:     opts.Credentials,
		now:             now,
	}
}

func (p *Plugin) Manifest() schemas.ProviderPluginManifest {
	homepage := "https://www.ovhcloud.com/en/public-cloud/"
	region := "bhs"
	image := defaultImageID
	notes := "Uses OVHcloud signed API credentials supplied by CAAM; existing-VPS onboarding remains the default path."
	return schemas.ProviderPluginManifest{
		SchemaVersion: 1,
		ProviderId:    ProviderID,
		DisplayName:   "OVHcloud Public Cloud",
		Homepage:      &homepage,
		AuthMode:      schemas.ApiToken,
		DefaultRegion: &region,
		DefaultImage:  &image,
		Notes:         &notes,
		Capabilities: []schemas.ProviderPluginManifestCapabilities{
			schemas.VpsListRegions,
			schemas.VpsListSizes,
			schemas.VpsEstimateCost,
			schemas.VpsCreate,
			schemas.VpsDestroy,
		},
	}
}

func (p *Plugin) ListRegions(ctx context.Context) ([]schemas.ProviderRegion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]schemas.ProviderRegion{}, regionCatalog...), nil
}

func (p *Plugin) ListSizes(ctx context.Context, regionID string) ([]schemas.ProviderSize, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !knownRegion(regionID) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRegion, regionID)
	}
	out := make([]schemas.ProviderSize, 0, len(sizeCatalog))
	for _, size := range sizeCatalog {
		out = append(out, size.toSchema())
	}
	return out, nil
}

func (p *Plugin) EstimateMonthlyCost(ctx context.Context, opts schemas.ProviderEstimateCostOpts) (*schemas.ProviderCostEstimate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !knownRegion(opts.Region) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRegion, opts.Region)
	}
	size, ok := lookupSize(opts.Size)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSize, opts.Size)
	}
	bandwidthTB := float32(1)
	if opts.BandwidthTBExpected != nil && *opts.BandwidthTBExpected > 0 {
		bandwidthTB = *opts.BandwidthTBExpected
	}
	compute := size.monthlyUSD - size.storageUSD - size.bandwidthUSD
	if compute < 0 {
		compute = 0
	}
	breakdown := []schemas.ProviderCostLineItem{
		{Label: "compute", Usd: compute},
		{Label: "storage", Usd: size.storageUSD},
		{Label: "bandwidth", Usd: size.bandwidthUSD},
	}
	if bandwidthTB > float32(size.bandwidthTB) {
		overage := (bandwidthTB - float32(size.bandwidthTB)) * 1
		breakdown = append(breakdown, schemas.ProviderCostLineItem{Label: "bandwidth-overage-estimate", Usd: overage})
	}
	total := sumCost(breakdown)
	currency := "USD"
	return &schemas.ProviderCostEstimate{
		Usd:            total,
		Currency:       &currency,
		Breakdown:      breakdown,
		CatalogVersion: catalogVersion,
		EstimatedAt:    p.now().UTC(),
	}, nil
}

func (p *Plugin) CreateInstance(ctx context.Context, opts schemas.ProviderCreateInstanceOpts) (*schemas.ProviderInstance, error) {
	if err := validateCreateOpts(opts); err != nil {
		return nil, err
	}
	if !knownRegion(opts.Region) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRegion, opts.Region)
	}
	size, ok := lookupSize(opts.Size)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSize, opts.Size)
	}
	if existing, err := p.findInstanceByName(ctx, opts.Name, opts.Region); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	sshKeyID, createdKey, err := p.ensureSSHKey(ctx, opts.Name, opts.SshPubKey)
	if err != nil {
		return nil, err
	}
	inst, err := p.createComputeInstance(ctx, opts, size, sshKeyID)
	if err != nil {
		if createdKey {
			_ = p.deleteSSHKey(context.WithoutCancel(ctx), sshKeyID)
		}
		return nil, err
	}
	return inst, nil
}

func (p *Plugin) DestroyInstance(ctx context.Context, instanceID string) (*schemas.ProviderDestroyResult, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("%w: instance id is required", ErrInvalidRequest)
	}
	path, err := p.projectPath("instance", instanceID)
	if err != nil {
		return nil, err
	}
	err = p.do(ctx, http.MethodDelete, path, nil, nil, nil)
	if err != nil {
		var perr *ProviderError
		if errors.As(err, &perr) && perr.StatusCode == http.StatusNotFound {
			notes := "instance not found; treated as already destroyed"
			return &schemas.ProviderDestroyResult{Ok: true, InstanceId: instanceID, Notes: &notes}, nil
		}
		return nil, err
	}
	notes := "OVHcloud instance deletion requested"
	return &schemas.ProviderDestroyResult{Ok: true, InstanceId: instanceID, Notes: &notes}, nil
}

func (p *Plugin) findInstanceByName(ctx context.Context, name, region string) (*schemas.ProviderInstance, error) {
	path, err := p.projectPath("instance")
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("region", regionAPIName(region))
	var out []ovhInstance
	if err := p.do(ctx, http.MethodGet, path, query, nil, &out); err != nil {
		return nil, err
	}
	for _, inst := range out {
		if inst.Name == name && mapStatus(inst.Status) != schemas.Destroyed {
			return mapInstance(inst, p.now()), nil
		}
	}
	return nil, nil
}

func (p *Plugin) ensureSSHKey(ctx context.Context, name, sshPubKey string) (string, bool, error) {
	path, err := p.projectPath("sshkey")
	if err != nil {
		return "", false, err
	}
	var keys []ovhSSHKey
	if err := p.do(ctx, http.MethodGet, path, nil, nil, &keys); err != nil {
		return "", false, err
	}
	normalizedKey := strings.TrimSpace(sshPubKey)
	for _, key := range keys {
		if strings.TrimSpace(key.PublicKey) == normalizedKey && strings.TrimSpace(key.ID) != "" {
			return key.ID, false, nil
		}
	}
	body := map[string]string{
		"name":      "hoopoe-" + safeName(name) + "-ssh",
		"publicKey": normalizedKey,
	}
	var key ovhSSHKey
	if err := p.do(ctx, http.MethodPost, path, nil, body, &key); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(key.ID) == "" {
		return "", false, fmt.Errorf("%w: missing ssh key response", ErrProviderUnavailable)
	}
	return key.ID, true, nil
}

func (p *Plugin) createComputeInstance(ctx context.Context, opts schemas.ProviderCreateInstanceOpts, size ovhSize, sshKeyID string) (*schemas.ProviderInstance, error) {
	path, err := p.projectPath("instance")
	if err != nil {
		return nil, err
	}
	imageID := strings.TrimSpace(opts.ImageId)
	if imageID == "" {
		imageID = defaultImageID
	}
	body := map[string]any{
		"flavorId": size.flavorID,
		"imageId":  imageID,
		"name":     opts.Name,
		"region":   regionAPIName(opts.Region),
		"sshKeyId": sshKeyID,
	}
	if p.publicNetworkID != "" {
		body["networks"] = []map[string]string{{"networkId": p.publicNetworkID}}
	}
	var inst ovhInstance
	if err := p.do(ctx, http.MethodPost, path, nil, body, &inst); err != nil {
		return nil, err
	}
	if strings.TrimSpace(inst.ID) == "" {
		return nil, fmt.Errorf("%w: missing instance response", ErrProviderUnavailable)
	}
	mapped := mapInstance(inst, p.now())
	mapped.Size = size.id
	mapped.Region = opts.Region
	mapped.MonthlyUSD = size.monthlyUSD
	return mapped, nil
}

func (p *Plugin) deleteSSHKey(ctx context.Context, sshKeyID string) error {
	path, err := p.projectPath("sshkey", sshKeyID)
	if err != nil {
		return err
	}
	err = p.do(ctx, http.MethodDelete, path, nil, nil, nil)
	if err != nil {
		var perr *ProviderError
		if errors.As(err, &perr) && perr.StatusCode == http.StatusNotFound {
			return nil
		}
	}
	return err
}

func validateCreateOpts(opts schemas.ProviderCreateInstanceOpts) error {
	if strings.TrimSpace(opts.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(opts.Region) == "" {
		return fmt.Errorf("%w: region is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(opts.Size) == "" {
		return fmt.Errorf("%w: size is required", ErrInvalidRequest)
	}
	if !validSSHPublicKey(opts.SshPubKey) {
		return fmt.Errorf("%w: ssh public key is required", ErrInvalidRequest)
	}
	return nil
}

func validSSHPublicKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.HasPrefix(key, "ssh-ed25519 ") || strings.HasPrefix(key, "ssh-rsa ")
}

func (p *Plugin) projectPath(parts ...string) (string, error) {
	if p.projectID == "" {
		return "", ErrProjectRequired
	}
	segments := []string{"/cloud/project", url.PathEscape(p.projectID)}
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			segments = append(segments, url.PathEscape(part))
		}
	}
	return strings.Join(segments, "/"), nil
}

func (p *Plugin) do(ctx context.Context, method, path string, query url.Values, requestBody any, responseBody any) error {
	creds, err := p.loadCredentials(ctx)
	if err != nil {
		return err
	}
	var bodyBytes []byte
	var body io.Reader
	if requestBody != nil {
		bodyBytes, err = json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(bodyBytes)
	}
	endpoint := p.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	timestamp := p.now().Unix()
	req.Header.Set("X-Ovh-Application", creds.ApplicationKey)
	req.Header.Set("X-Ovh-Consumer", creds.ConsumerKey)
	req.Header.Set("X-Ovh-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-Ovh-Signature", ovhSignature(creds, method, endpoint, bodyBytes, timestamp))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-request-id", newRequestID())
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newProviderError(method, path, resp.StatusCode, data)
	}
	if responseBody == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, responseBody); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrProviderUnavailable, err)
	}
	return nil
}

func (p *Plugin) loadCredentials(ctx context.Context) (Credentials, error) {
	if p.credentials == nil {
		return Credentials{}, ErrAuthRequired
	}
	creds, err := p.credentials.Credentials(ctx)
	if err != nil {
		return Credentials{}, err
	}
	creds.ApplicationKey = strings.TrimSpace(creds.ApplicationKey)
	creds.ApplicationSecret = strings.TrimSpace(creds.ApplicationSecret)
	creds.ConsumerKey = strings.TrimSpace(creds.ConsumerKey)
	if creds.ApplicationKey == "" || creds.ApplicationSecret == "" || creds.ConsumerKey == "" {
		return Credentials{}, ErrAuthRequired
	}
	return creds, nil
}

func ovhSignature(creds Credentials, method, endpoint string, body []byte, timestamp int64) string {
	payload := fmt.Sprintf("%s+%s+%s+%s+%s+%d",
		creds.ApplicationSecret,
		creds.ConsumerKey,
		strings.ToUpper(method),
		endpoint,
		string(body),
		timestamp,
	)
	// #nosec G401 -- OVHcloud API v1 requires this SHA-1 signature format.
	sum := sha1.Sum([]byte(payload))
	return "$1$" + hex.EncodeToString(sum[:])
}

type ProviderError struct {
	Method     string
	Path       string
	StatusCode int
	ErrorClass string
	Body       string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("ovh: %s %s failed: %s (%d)", e.Method, e.Path, e.ErrorClass, e.StatusCode)
}

func newProviderError(method, path string, status int, body []byte) *ProviderError {
	return &ProviderError{
		Method:     method,
		Path:       path,
		StatusCode: status,
		ErrorClass: classifyStatus(status),
		Body:       strings.TrimSpace(string(body)),
	}
}

func classifyStatus(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "auth"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusConflict:
		return "conflict"
	case http.StatusServiceUnavailable:
		return "provider_unavailable"
	default:
		if status >= 500 {
			return "provider_unavailable"
		}
		if status >= 400 {
			return "request_rejected"
		}
		return "unknown"
	}
}

type ovhSSHKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

type ovhIPAddress struct {
	IP      string `json:"ip"`
	Version int    `json:"version"`
}

type ovhInstance struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	FlavorID    string         `json:"flavorId"`
	Region      string         `json:"region"`
	Status      string         `json:"status"`
	CreatedAt   time.Time      `json:"created"`
	IP          string         `json:"ip"`
	IPAddresses []ovhIPAddress `json:"ipAddresses"`
	Flavor      *struct {
		ID string `json:"id"`
	} `json:"flavor"`
}

func mapInstance(inst ovhInstance, fallbackNow time.Time) *schemas.ProviderInstance {
	created := inst.CreatedAt
	if created.IsZero() {
		created = fallbackNow
	}
	ip := strings.TrimSpace(inst.IP)
	for _, candidate := range inst.IPAddresses {
		if strings.TrimSpace(candidate.IP) != "" && (candidate.Version == 4 || ip == "") {
			ip = candidate.IP
			if candidate.Version == 4 {
				break
			}
		}
	}
	var ipPtr *string
	if strings.TrimSpace(ip) != "" {
		ipPtr = &ip
	}
	flavorID := inst.FlavorID
	if flavorID == "" && inst.Flavor != nil {
		flavorID = inst.Flavor.ID
	}
	sizeID := sizeIDForFlavor(flavorID)
	monthly := float32(0)
	if size, ok := lookupSize(sizeID); ok {
		monthly = size.monthlyUSD
	}
	return &schemas.ProviderInstance{
		InstanceId: inst.ID,
		Status:     mapStatus(inst.Status),
		CreatedAt:  created.UTC(),
		Region:     regionIDForAPIName(inst.Region),
		Size:       sizeID,
		Ip:         ipPtr,
		MonthlyUSD: monthly,
	}
}

func mapStatus(status string) schemas.ProviderInstanceStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "running":
		return schemas.Running
	case "error", "failed":
		return schemas.Failed
	case "deleted", "deleting", "destroyed":
		return schemas.Destroyed
	default:
		return schemas.Provisioning
	}
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(b[:])
	return hexed[:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:]
}

func safeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "instance"
	}
	if len(out) > 48 {
		return out[:48]
	}
	return out
}

func sumCost(items []schemas.ProviderCostLineItem) float32 {
	var total float32
	for _, item := range items {
		total += item.Usd
	}
	return total
}
