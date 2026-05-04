// Package inventory builds the daemon's onboarding/tool-inventory view from
// canonical capability reports plus CAAM's subscription/account inventory.
package inventory

import (
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const SchemaVersion = 1

type Snapshot struct {
	SchemaVersion             int                      `json:"schemaVersion"`
	SnapshotAt                string                   `json:"snapshotAt"`
	CapabilitiesSchemaVersion int                      `json:"capabilitiesSchemaVersion"`
	DaemonAPIVersion          string                   `json:"daemonApiVersion"`
	FixturesVersion           string                   `json:"fixturesVersion,omitempty"`
	Tools                     []Tool                   `json:"tools"`
	SubscriptionVerification  SubscriptionVerification `json:"subscriptionVerification"`
	Warnings                  []Warning                `json:"warnings,omitempty"`
}

type Tool struct {
	ID              capabilities.ToolID                `json:"id"`
	Name            string                             `json:"name"`
	Repo            string                             `json:"repo,omitempty"`
	Version         string                             `json:"version"`
	Source          string                             `json:"source"`
	Status          capabilities.CapabilityStatus      `json:"status"`
	Capabilities    map[string]capabilities.Capability `json:"capabilities"`
	CapabilityIDs   []string                           `json:"capabilityIds"`
	LastCheckedAt   string                             `json:"lastCheckedAt"`
	FixturesVersion string                             `json:"fixturesVersion,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type VerificationStatus string

const (
	VerificationOK          VerificationStatus = "ok"
	VerificationWarning     VerificationStatus = "warning"
	VerificationUnavailable VerificationStatus = "unavailable"
)

type SubscriptionVerification struct {
	Status                  VerificationStatus `json:"status"`
	CheckedAt               string             `json:"checkedAt"`
	RequiredTools           []SubscriptionTool `json:"requiredTools"`
	TotalAccountCount       int                `json:"totalAccountCount"`
	TotalAvailableAccounts  int                `json:"totalAvailableAccounts"`
	SignedInCount           int                `json:"signedInCount"`
	ZeroSubscriptionWarning bool               `json:"zeroSubscriptionWarning"`
	DetectedAgents          []DetectedAgent    `json:"detectedAgents,omitempty"`
	Warnings                []Warning          `json:"warnings,omitempty"`
}

type SubscriptionTool struct {
	Tool                 string           `json:"tool"`
	DisplayName          string           `json:"displayName"`
	ExpectedSubscription string           `json:"expectedSubscription"`
	SignedIn             bool             `json:"signedIn"`
	ActiveProfile        string           `json:"activeProfile,omitempty"`
	Health               string           `json:"health,omitempty"`
	AccountCount         int              `json:"accountCount"`
	AvailableAccounts    int              `json:"availableAccounts"`
	Profiles             []AccountProfile `json:"profiles,omitempty"`
	Limits               []LimitSummary   `json:"limits,omitempty"`
	DetectedAgent        *DetectedAgent   `json:"detectedAgent,omitempty"`
}

type AccountProfile struct {
	Name          string     `json:"name"`
	Active        bool       `json:"active"`
	Favorite      bool       `json:"favorite"`
	Health        string     `json:"health,omitempty"`
	Available     bool       `json:"available"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
	CooldownUntil *time.Time `json:"cooldownUntil,omitempty"`
}

type LimitSummary struct {
	Provider       string     `json:"provider"`
	Profile        string     `json:"profile,omitempty"`
	UsedPct        float64    `json:"usedPct,omitempty"`
	WindowResetsAt *time.Time `json:"windowResetsAt,omitempty"`
	BurnRate       string     `json:"burnRate,omitempty"`
	Notes          string     `json:"notes,omitempty"`
}

type DetectedAgent struct {
	Tool        string `json:"tool"`
	DisplayName string `json:"displayName"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	BinaryPath  string `json:"binaryPath,omitempty"`
}
