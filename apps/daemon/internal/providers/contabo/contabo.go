// Package contabo implements Hoopoe's built-in Contabo VPS provider plugin.
package contabo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/logger"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	ProviderID     = "contabo"
	defaultBaseURL = "https://api.contabo.com/v1"
	defaultImageID = "ubuntu-24.04"
	catalogVersion = "contabo-2026-05-04"
)

var (
	ErrInvalidRequest      = errors.New("contabo: invalid request")
	ErrAuthRequired        = errors.New("contabo: bearer token source required")
	ErrUnknownRegion       = errors.New("contabo: unknown region")
	ErrUnknownSize         = errors.New("contabo: unknown size")
	ErrProviderUnavailable = errors.New("contabo: provider unavailable")
)

type TokenSource interface {
	BearerToken(ctx context.Context) (string, error)
}

type TokenSourceFunc func(ctx context.Context) (string, error)

func (f TokenSourceFunc) BearerToken(ctx context.Context) (string, error) {
	return f(ctx)
}

type AuditAppender interface {
	Append(audit.Entry) (audit.Entry, []redaction.TraceEvent, error)
}

type Options struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      TokenSource
	Logger     *logger.Logger
	Audit      AuditAppender
	Now        func() time.Time
}

type Plugin struct {
	baseURL string
	client  *http.Client
	token   TokenSource
	log     *logger.Logger
	audit   AuditAppender
	now     func() time.Time
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
		baseURL: baseURL,
		client:  client,
		token:   opts.Token,
		log:     opts.Logger,
		audit:   opts.Audit,
		now:     now,
	}
}

func (p *Plugin) Manifest() schemas.ProviderPluginManifest {
	homepage := "https://contabo.com/en/contabo-api/"
	region := "us-east-1"
	image := defaultImageID
	notes := "Uses Contabo API bearer credentials supplied by CAAM; existing-VPS onboarding remains the default path."
	return schemas.ProviderPluginManifest{
		SchemaVersion: 1,
		ProviderId:    ProviderID,
		DisplayName:   "Contabo Cloud VPS",
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
	if surcharge := size.regionSurchargeUSD(opts.Region); surcharge > 0 {
		breakdown = append(breakdown, schemas.ProviderCostLineItem{Label: "datacenter-surcharge", Usd: surcharge})
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

func (p *Plugin) CreateInstance(ctx context.Context, opts schemas.ProviderCreateInstanceOpts) (inst *schemas.ProviderInstance, err error) {
	started := p.now().UTC()
	auditData := map[string]any{
		"provider": ProviderID,
		"name":     opts.Name,
		"region":   opts.Region,
		"size":     opts.Size,
		"imageId":  opts.ImageId,
	}
	defer func() {
		if inst != nil {
			auditData["instanceId"] = inst.InstanceId
			auditData["status"] = inst.Status
		}
		if auditErr := p.auditProviderAction("provider.contabo.create_instance", started, err, auditData); err == nil && auditErr != nil {
			err = auditErr
		}
	}()
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
	if existing, err := p.findInstanceByDisplayName(ctx, opts.Name); err != nil {
		return nil, err
	} else if existing != nil {
		auditData["existing"] = true
		return existing, nil
	}
	secretID, err := p.createSSHKeySecret(ctx, opts.Name, opts.SshPubKey)
	if err != nil {
		return nil, err
	}
	inst, err = p.createComputeInstance(ctx, opts, size, secretID)
	if err != nil {
		_ = p.deleteSecret(context.WithoutCancel(ctx), secretID)
		return nil, err
	}
	return inst, nil
}

func (p *Plugin) DestroyInstance(ctx context.Context, instanceID string) (result *schemas.ProviderDestroyResult, err error) {
	instanceID = strings.TrimSpace(instanceID)
	started := p.now().UTC()
	auditData := map[string]any{
		"provider":   ProviderID,
		"instanceId": instanceID,
	}
	defer func() {
		if result != nil {
			auditData["ok"] = result.Ok
		}
		if auditErr := p.auditProviderAction("provider.contabo.destroy_instance", started, err, auditData); err == nil && auditErr != nil {
			err = auditErr
		}
	}()
	if instanceID == "" {
		return nil, fmt.Errorf("%w: instance id is required", ErrInvalidRequest)
	}
	id, err := strconv.ParseInt(instanceID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: Contabo instance ids must be numeric", ErrInvalidRequest)
	}
	var out contaboResponse[contaboCancelResult]
	err = p.do(ctx, http.MethodPost, fmt.Sprintf("/compute/instances/%d/cancel", id), nil, nil, &out)
	if err != nil {
		var perr *ProviderError
		if errors.As(err, &perr) && perr.StatusCode == http.StatusNotFound {
			notes := "instance not found; treated as already destroyed"
			return &schemas.ProviderDestroyResult{Ok: true, InstanceId: instanceID, Notes: &notes}, nil
		}
		return nil, err
	}
	notes := "Contabo cancellation requested"
	if len(out.Data) > 0 && out.Data[0].CancelDate != "" {
		notes = "Contabo cancellation requested for " + out.Data[0].CancelDate
	}
	return &schemas.ProviderDestroyResult{Ok: true, InstanceId: instanceID, Notes: &notes}, nil
}

func (p *Plugin) findInstanceByDisplayName(ctx context.Context, name string) (*schemas.ProviderInstance, error) {
	query := url.Values{}
	query.Set("displayName", name)
	var out contaboResponse[contaboInstance]
	if err := p.do(ctx, http.MethodGet, "/compute/instances", query, nil, &out); err != nil {
		return nil, err
	}
	for _, inst := range out.Data {
		if inst.DisplayName == name {
			return mapInstance(inst), nil
		}
	}
	return nil, nil
}

func (p *Plugin) createSSHKeySecret(ctx context.Context, name, sshPubKey string) (int64, error) {
	body := map[string]string{
		"name":  "hoopoe-" + safeName(name) + "-ssh",
		"type":  "ssh",
		"value": sshPubKey,
	}
	var out contaboResponse[contaboSecret]
	if err := p.do(ctx, http.MethodPost, "/secrets", nil, body, &out); err != nil {
		return 0, err
	}
	if len(out.Data) == 0 {
		return 0, fmt.Errorf("%w: missing secret response", ErrProviderUnavailable)
	}
	id, err := strconv.ParseInt(out.Data[0].SecretID, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: invalid secret id", ErrProviderUnavailable)
	}
	return id, nil
}

func (p *Plugin) createComputeInstance(ctx context.Context, opts schemas.ProviderCreateInstanceOpts, size contaboSize, secretID int64) (*schemas.ProviderInstance, error) {
	imageID := strings.TrimSpace(opts.ImageId)
	if imageID == "" {
		imageID = defaultImageID
	}
	body := map[string]any{
		"imageId":     imageID,
		"productId":   size.productID,
		"region":      regionSlug(opts.Region),
		"sshKeys":     []int64{secretID},
		"displayName": opts.Name,
		"defaultUser": "root",
	}
	var out contaboResponse[contaboInstance]
	if err := p.do(ctx, http.MethodPost, "/compute/instances", nil, body, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("%w: missing instance response", ErrProviderUnavailable)
	}
	inst := mapInstance(out.Data[0])
	inst.Size = size.id
	inst.Region = opts.Region
	inst.MonthlyUSD = size.monthlyUSD
	return inst, nil
}

func (p *Plugin) deleteSecret(ctx context.Context, secretID int64) error {
	var out map[string]any
	err := p.do(ctx, http.MethodDelete, fmt.Sprintf("/secrets/%d", secretID), nil, nil, &out)
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

func (p *Plugin) do(ctx context.Context, method, path string, query url.Values, requestBody any, responseBody any) error {
	if p.token == nil {
		return ErrAuthRequired
	}
	token, err := p.token.BearerToken(ctx)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrAuthRequired
	}
	var body io.Reader
	if requestBody != nil {
		data, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	endpoint := p.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	started := p.now().UTC()
	statusCode := 0
	errorClass := ""
	defer func() {
		p.logAPICall(ctx, method, path, started, statusCode, errorClass)
	}()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		errorClass = "request_build_failed"
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-request-id", newRequestID())
	resp, err := p.client.Do(req)
	if err != nil {
		errorClass = "transport"
		return err
	}
	defer resp.Body.Close()
	statusCode = resp.StatusCode
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		errorClass = "response_read_failed"
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		perr := newProviderError(method, path, resp.StatusCode, data)
		errorClass = perr.ErrorClass
		return perr
	}
	if responseBody == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, responseBody); err != nil {
		errorClass = "decode_failed"
		return fmt.Errorf("%w: decode response: %v", ErrProviderUnavailable, err)
	}
	return nil
}

func (p *Plugin) logAPICall(ctx context.Context, method, path string, started time.Time, statusCode int, errorClass string) {
	log := p.log
	if log == nil {
		log = logger.FromContext(ctx)
	}
	if log == nil {
		return
	}
	fields := []logger.Field{
		{Key: "plugin", Value: ProviderID},
		{Key: "method", Value: method},
		{Key: "path", Value: path},
		{Key: "durationMs", Value: p.now().UTC().Sub(started).Milliseconds()},
	}
	if statusCode > 0 {
		fields = append(fields, logger.Field{Key: "statusCode", Value: statusCode})
	}
	if errorClass != "" {
		fields = append(fields, logger.Field{Key: "errorClass", Value: errorClass})
		log.With(
			logger.Field{Key: "component", Value: logger.ComponentDaemonAdapters},
			logger.Field{Key: "subsystem", Value: "providers.contabo"},
		).Warn("contabo api call failed", fields...)
		return
	}
	log.With(
		logger.Field{Key: "component", Value: logger.ComponentDaemonAdapters},
		logger.Field{Key: "subsystem", Value: "providers.contabo"},
	).Info("contabo api call", fields...)
}

func (p *Plugin) auditProviderAction(action string, started time.Time, actionErr error, data map[string]any) error {
	if p.audit == nil {
		return nil
	}
	result := audit.ResultSuccess
	if actionErr != nil {
		result = audit.ResultFailure
		data["errorClass"] = providerErrorClass(actionErr)
		data["error"] = actionErr.Error()
	}
	data["durationMs"] = p.now().UTC().Sub(started).Milliseconds()
	_, _, err := p.audit.Append(audit.Entry{
		ProjectID: audit.GlobalProjectID,
		Actor:     audit.Actor{Kind: audit.ActorAdapter, ID: ProviderID},
		Action:    action,
		Result:    result,
		Data:      data,
	})
	if err != nil {
		return fmt.Errorf("contabo: audit provider action: %w", err)
	}
	return nil
}

func providerErrorClass(err error) string {
	if err == nil {
		return ""
	}
	var perr *ProviderError
	if errors.As(err, &perr) {
		return perr.ErrorClass
	}
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request"
	case errors.Is(err, ErrAuthRequired):
		return "auth"
	case errors.Is(err, ErrUnknownRegion), errors.Is(err, ErrUnknownSize):
		return "request_rejected"
	case errors.Is(err, ErrProviderUnavailable):
		return "provider_unavailable"
	default:
		return "unknown"
	}
}

type ProviderError struct {
	Method     string
	Path       string
	StatusCode int
	ErrorClass string
	Body       string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("contabo: %s %s failed: %s (%d)", e.Method, e.Path, e.ErrorClass, e.StatusCode)
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

type contaboResponse[T any] struct {
	Data []T `json:"data"`
}

type contaboSecret struct {
	SecretID string `json:"secretId"`
}

type contaboCancelResult struct {
	CancelDate string `json:"cancelDate"`
}

type contaboInstance struct {
	InstanceID  int64     `json:"instanceId"`
	DisplayName string    `json:"displayName"`
	Name        string    `json:"name"`
	ProductID   string    `json:"productId"`
	Region      string    `json:"region"`
	RegionSlug  string    `json:"regionSlug"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdDate"`
	IPConfig    struct {
		V4 *struct {
			IP string `json:"ip"`
		} `json:"v4"`
		V6 *struct {
			IP string `json:"ip"`
		} `json:"v6"`
	} `json:"ipConfig"`
}

func mapInstance(inst contaboInstance) *schemas.ProviderInstance {
	created := inst.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	ip := ""
	if inst.IPConfig.V4 != nil {
		ip = inst.IPConfig.V4.IP
	} else if inst.IPConfig.V6 != nil {
		ip = inst.IPConfig.V6.IP
	}
	var ipPtr *string
	if strings.TrimSpace(ip) != "" {
		ipPtr = &ip
	}
	sizeID := sizeIDForProduct(inst.ProductID)
	regionID := regionIDForSlug(firstNonEmpty(inst.RegionSlug, inst.Region))
	monthly := float32(0)
	if size, ok := lookupSize(sizeID); ok {
		monthly = size.monthlyUSD
	}
	return &schemas.ProviderInstance{
		InstanceId: strconv.FormatInt(inst.InstanceID, 10),
		Status:     mapStatus(inst.Status),
		CreatedAt:  created.UTC(),
		Region:     regionID,
		Size:       sizeID,
		Ip:         ipPtr,
		MonthlyUSD: monthly,
	}
}

func mapStatus(status string) schemas.ProviderInstanceStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return schemas.Running
	case "error", "failed", "product_not_available", "verification_required":
		return schemas.Failed
	case "cancelled", "canceled", "destroyed":
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sumCost(items []schemas.ProviderCostLineItem) float32 {
	var total float32
	for _, item := range items {
		total += item.Usd
	}
	return total
}
