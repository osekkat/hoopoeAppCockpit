// Hoopoe-owned. Main-process client for the daemon's `/v1/capabilities`
// route. Wraps fetch + caches the registry snapshot + exposes
// capability-gated feature decisions to the rest of the main process and
// (via IPC) the renderer.
//
// hp-4vk: every stage route in the renderer reads capability flags from
// /v1/capabilities and renders one of available/degraded/unavailable/
// blocked-by-policy states. This client is the renderer's path to those
// flags. Pure data — no IPC plumbing here; the wiring into the
// daemon.request dispatcher lives in apps/desktop/src/main/BackendLifecycle
// once hp-1ry's production daemon RPC layer lands.
//
// Cross-references:
//   - apps/desktop/src/capabilities/ — the renderer-side `decideFeature`
//     and `FEATURE_CATALOG` from hp-r33.
//   - apps/daemon/internal/api/capabilities.go — the route this client
//     consumes.

import {
  decideFeature,
  determineFeature,
  emptyRegistry,
  FEATURE_CATALOG,
  type CapabilityRegistry,
  type CompatibilityReport,
  type FeatureCapabilityRequirement,
  type FeatureDecision,
  type FeatureId,
} from "../capabilities/index.ts";

/** Function-shaped fetcher so tests inject a fixture without a real
 *  daemon. Production wires this to global `fetch` with the SSH-tunneled
 *  base URL. */
export type CapabilitiesFetcher = (path: string) => Promise<Response>;

export interface CapabilitiesClientOptions {
  readonly fetcher: CapabilitiesFetcher;
  readonly now?: () => Date;
  /** Optional cap on how stale a cached snapshot can be before a fresh
   *  fetch is forced. Defaults to 5s — short enough that startup races
   *  with adapter probes never stall the renderer; long enough that a
   *  burst of capability checks doesn't hammer the daemon. */
  readonly maxStaleMs?: number;
  /** Hook for test instrumentation. Fires every time the client
   *  successfully refreshes the cached registry. */
  readonly onSnapshot?: (registry: CapabilityRegistry) => void;
}

const DEFAULT_MAX_STALE_MS = 5_000;

export class CapabilitiesClient {
  private readonly fetcher: CapabilitiesFetcher;
  private readonly now: () => Date;
  private readonly maxStaleMs: number;
  private readonly onSnapshot: ((r: CapabilityRegistry) => void) | undefined;

  private cached: CapabilityRegistry = emptyRegistry();
  private cachedAtMs = 0;
  private inflight: Promise<CapabilityRegistry> | null = null;

  constructor(options: CapabilitiesClientOptions) {
    this.fetcher = options.fetcher;
    this.now = options.now ?? (() => new Date());
    this.maxStaleMs = options.maxStaleMs ?? DEFAULT_MAX_STALE_MS;
    this.onSnapshot = options.onSnapshot;
  }

  /** Returns the cached snapshot (synchronous). The caller is responsible
   *  for ensuring `refresh()` has been awaited before render-blocking
   *  decisions. */
  snapshot(): CapabilityRegistry {
    return this.cached;
  }

  /** Refreshes the cached snapshot from `/v1/capabilities`. Coalesces
   *  concurrent calls — only one HTTP request flies at a time. */
  async refresh(): Promise<CapabilityRegistry> {
    if (this.inflight) return this.inflight;
    this.inflight = this.fetchSnapshot();
    try {
      return await this.inflight;
    } finally {
      this.inflight = null;
    }
  }

  /** Returns the cached snapshot if fresh; otherwise fetches a new one.
   *  "Fresh" is `now - cachedAt < maxStaleMs` AND the cache has been
   *  populated at least once. */
  async ensureFresh(): Promise<CapabilityRegistry> {
    const ageMs = this.now().getTime() - this.cachedAtMs;
    if (this.cachedAtMs > 0 && ageMs < this.maxStaleMs) {
      return this.cached;
    }
    return this.refresh();
  }

  /** Fetches and caches `/v1/compatibility`. Compatibility is fetched
   *  separately because callers want to trigger `unsupportedClientWarnings`
   *  on app boot independently of the per-feature gating. */
  async fetchCompatibility(): Promise<CompatibilityReport> {
    const res = await this.fetcher("/v1/compatibility");
    if (!res.ok) {
      throw new Error(`/v1/compatibility returned ${res.status}`);
    }
    return (await res.json()) as CompatibilityReport;
  }

  /** Resolves a feature id from the renderer's `FEATURE_CATALOG` against
   *  the current cached snapshot. */
  decide(featureId: FeatureId): FeatureDecision {
    return decideFeature(this.cached, featureId);
  }

  /** Resolves an arbitrary requirement against the cached snapshot. Used
   *  by tending jobs that build their requirement at runtime. */
  decideRequirement(requirement: FeatureCapabilityRequirement): FeatureDecision {
    return determineFeature(this.cached, requirement);
  }

  /** Resolves every feature in the catalog. Diagnostics consumes this. */
  decideAll(): readonly FeatureDecision[] {
    return Object.keys(FEATURE_CATALOG).map((featureId) => this.decide(featureId as FeatureId));
  }

  private async fetchSnapshot(): Promise<CapabilityRegistry> {
    const res = await this.fetcher("/v1/capabilities");
    if (!res.ok) {
      throw new Error(`/v1/capabilities returned ${res.status}`);
    }
    const registry = (await res.json()) as CapabilityRegistry;
    if (registry.schemaVersion !== 1) {
      throw new Error(
        `/v1/capabilities schemaVersion=${registry.schemaVersion} != expected 1`,
      );
    }
    this.cached = registry;
    this.cachedAtMs = this.now().getTime();
    if (this.onSnapshot) this.onSnapshot(registry);
    return registry;
  }
}
