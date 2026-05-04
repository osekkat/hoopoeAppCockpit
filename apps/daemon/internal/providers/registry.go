// Package providers owns the in-process VPS provider plugin registry.
package providers

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

var ErrInvalidPlugin = errors.New("providers: invalid plugin")

type Registry struct {
	mu      sync.RWMutex
	plugins []schemas.ProviderPlugin
	byID    map[schemas.ProviderId]schemas.ProviderPlugin
}

var _ schemas.ProviderRegistry = (*Registry)(nil)

func NewRegistry(plugins ...schemas.ProviderPlugin) (*Registry, error) {
	registry := &Registry{
		byID: make(map[schemas.ProviderId]schemas.ProviderPlugin),
	}
	for _, plugin := range plugins {
		if err := registry.Register(plugin); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(plugin schemas.ProviderPlugin) error {
	if plugin == nil {
		return fmt.Errorf("%w: nil plugin", ErrInvalidPlugin)
	}
	manifest := plugin.Manifest()
	id := schemas.ProviderId(strings.TrimSpace(string(manifest.ProviderId)))
	if err := validateManifest(manifest); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID == nil {
		r.byID = make(map[schemas.ProviderId]schemas.ProviderPlugin)
	}
	if _, exists := r.byID[id]; exists {
		return fmt.Errorf("%w: %s", schemas.ErrProviderAlreadyRegistered, id)
	}
	r.byID[id] = plugin
	r.plugins = append(r.plugins, plugin)
	return nil
}

func (r *Registry) Get(id schemas.ProviderId) (schemas.ProviderPlugin, error) {
	id = schemas.ProviderId(strings.TrimSpace(string(id)))
	r.mu.RLock()
	defer r.mu.RUnlock()
	plugin, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", schemas.ErrProviderNotFound, id)
	}
	return plugin, nil
}

func (r *Registry) List() []schemas.ProviderPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]schemas.ProviderPlugin{}, r.plugins...)
}

func RequireCapability(plugin schemas.ProviderPlugin, capability schemas.ProviderPluginManifestCapabilities) error {
	if plugin == nil {
		return fmt.Errorf("%w: nil plugin", ErrInvalidPlugin)
	}
	manifest := plugin.Manifest()
	if !manifest.HasCapability(capability) {
		return fmt.Errorf("%w: %s missing %s", schemas.ErrProviderMethodUnsupported, manifest.ProviderId, capability)
	}
	return nil
}

func validateManifest(manifest schemas.ProviderPluginManifest) error {
	if strings.TrimSpace(string(manifest.ProviderId)) == "" {
		return fmt.Errorf("%w: missing provider id", ErrInvalidPlugin)
	}
	if manifest.SchemaVersion <= 0 {
		return fmt.Errorf("%w: %s missing schema version", ErrInvalidPlugin, manifest.ProviderId)
	}
	if strings.TrimSpace(manifest.DisplayName) == "" {
		return fmt.Errorf("%w: %s missing display name", ErrInvalidPlugin, manifest.ProviderId)
	}
	if !manifest.AuthMode.Valid() {
		return fmt.Errorf("%w: %s invalid auth mode %q", ErrInvalidPlugin, manifest.ProviderId, manifest.AuthMode)
	}
	if len(manifest.Capabilities) == 0 {
		return fmt.Errorf("%w: %s declares no capabilities", ErrInvalidPlugin, manifest.ProviderId)
	}
	seen := make(map[schemas.ProviderPluginManifestCapabilities]bool, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if !capability.Valid() {
			return fmt.Errorf("%w: %s invalid capability %q", ErrInvalidPlugin, manifest.ProviderId, capability)
		}
		if seen[capability] {
			return fmt.Errorf("%w: %s duplicate capability %q", ErrInvalidPlugin, manifest.ProviderId, capability)
		}
		seen[capability] = true
	}
	return nil
}
