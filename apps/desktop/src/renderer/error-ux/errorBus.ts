// errorBus pub/sub primitive (hp-8dym).
//
// Single renderer-process channel. Features publish; surface
// containers subscribe. Coalesces identical errors (same source +
// problem.type within COALESCE_WINDOW_MS) and exposes the resulting
// stack in stable insertion order so React renders are deterministic.

import {
  defaultAutoDismissMs,
  defaultDismissible,
  deriveSeverity,
  deriveSurface,
} from "./classification.ts";
import type {
  ErrorBus,
  ErrorBusListener,
  ErrorBusSnapshot,
  ErrorPayload,
  PublishedError,
} from "./types.ts";

/** Window during which identical errors collapse into a single entry. */
export const COALESCE_WINDOW_MS = 2_000;

/** Maximum simultaneously visible toasts; the rest fall back to a
 *  "+ N more" overflow handled by the surface layer. */
export const MAX_VISIBLE_TOASTS = 5;

/** Maximum simultaneously visible banners. */
export const MAX_VISIBLE_BANNERS = 3;

interface InternalState {
  errors: PublishedError[];
  listeners: Set<ErrorBusListener>;
  nextId: number;
}

function makeState(): InternalState {
  return { errors: [], listeners: new Set(), nextId: 1 };
}

function coalesceKey(payload: ErrorPayload): string {
  return `${payload.source}::${payload.envelope.type}`;
}

function notify(state: InternalState): void {
  const snapshot: readonly PublishedError[] = state.errors.slice();
  for (const listener of state.listeners) {
    listener(snapshot);
  }
}

export function createErrorBus(): ErrorBus {
  const state = makeState();

  function publish(payload: ErrorPayload): string {
    const now = Date.now();
    const key = coalesceKey(payload);
    const existing = state.errors.find(
      (entry) =>
        coalesceKey(entry) === key && now - entry.publishedAt <= COALESCE_WINDOW_MS,
    );
    if (existing) {
      const idx = state.errors.indexOf(existing);
      const updated: PublishedError = {
        ...existing,
        coalescedCount: existing.coalescedCount + 1,
      };
      state.errors[idx] = updated;
      notify(state);
      return existing.id;
    }
    const severity = deriveSeverity(payload.envelope, payload.severity);
    const surface = deriveSurface(payload.envelope, payload.surfaceOverride);
    const id = `err-${state.nextId++}`;
    const dismissible = defaultDismissible(severity, surface, payload.hints?.dismissible);
    const autoDismissMs = defaultAutoDismissMs(
      severity,
      surface,
      payload.hints?.autoDismissMs,
    );
    const hints = {
      ...payload.hints,
      dismissible,
      ...(autoDismissMs !== null ? { autoDismissMs } : {}),
    };
    const entry: PublishedError = {
      ...payload,
      id,
      severity,
      surface,
      publishedAt: now,
      coalescedCount: 1,
      hints,
    };
    state.errors.push(entry);
    notify(state);
    return id;
  }

  function dismiss(id: string): void {
    const before = state.errors.length;
    state.errors = state.errors.filter((entry) => entry.id !== id);
    if (state.errors.length !== before) notify(state);
  }

  function dismissAll(): void {
    if (state.errors.length === 0) return;
    state.errors = [];
    notify(state);
  }

  function subscribe(listener: ErrorBusListener): () => void {
    state.listeners.add(listener);
    listener(state.errors.slice());
    return () => {
      state.listeners.delete(listener);
    };
  }

  function getSnapshot(): ErrorBusSnapshot {
    return { errors: state.errors.slice() };
  }

  function reset(): void {
    state.errors = [];
    state.listeners.clear();
    state.nextId = 1;
  }

  return { publish, dismiss, dismissAll, subscribe, getSnapshot, reset };
}

/** Singleton shared across the renderer. Tests should call `reset()`
 *  in afterEach blocks (or use `createErrorBus()` for isolated bus
 *  instances). */
export const errorBus: ErrorBus = createErrorBus();
