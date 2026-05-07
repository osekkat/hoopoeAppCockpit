package main

import (
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
)

// tending_capabilities.go wires the daemon's authoritative
// *capabilities.Registry into the scheduler's narrow
// scheduler.CapabilityChecker contract.
//
// hp-ktog: without this adapter, openTendingRegistry passes nil for
// RegistryConfig.Capabilities, which short-circuits the hp-8gq
// pre-dispatch capability gate (registry.go's `if r.capabilities != nil`
// branch). That makes the "enforce capabilities before dispatch"
// guarantee a TEST-ONLY feature: a job declaring CapabilitiesRequired
// dispatches in production even when the required capability is
// missing or blocked-by-policy. The hp-8gq commit (901588e) explicitly
// flagged the production wiring as a deferred follow-up; this is that
// follow-up.

type capabilityRegistryAdapter struct {
	r *capabilities.Registry
}

// LookupCapabilityStatus widens *capabilities.Registry's status enum
// into scheduler.CapabilityStatus (string-equivalent values, separate
// type so the scheduler doesn't import the capabilities package).
// A nil-wrapped registry returns (StatusMissing, false) so empty CLI
// boots block any capability-requiring job rather than dispatching
// blind — matches *capabilities.Registry.LookupCapabilityStatus on a
// nil receiver.
func (a capabilityRegistryAdapter) LookupCapabilityStatus(ref string) (scheduler.CapabilityStatus, bool) {
	if a.r == nil {
		return scheduler.CapabilityStatusMissing, false
	}
	status, ok := a.r.LookupCapabilityStatus(ref)
	if !ok {
		return scheduler.CapabilityStatusMissing, false
	}
	return scheduler.CapabilityStatus(string(status)), true
}

// newTendingCapabilityRegistry returns the *capabilities.Registry that
// backs the tending scheduler's pre-dispatch gate. The CLI wires an
// empty registry by default — production deploys (mock-flywheel mode,
// real daemon boot) populate the registry via the inventory service
// and the adapter readers see the same data. Empty-by-default is the
// safe choice: jobs without CapabilitiesRequired keep dispatching, but
// any job that declares a required capability is blocked by the gate
// until something registers a probe or report for it.
func newTendingCapabilityRegistry() *capabilities.Registry {
	return capabilities.New("v1")
}
