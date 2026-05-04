import { describe, expect, test } from "bun:test";
import { createErrorBus } from "./errorBus.ts";
import {
  buildRendererWindowErrorEnvelope,
  installRendererWindowErrorHandlers,
  publishRendererWindowError,
} from "./windowErrorHandlers.ts";

describe("hp-1te :: renderer window error handlers", () => {
  test("unhandledrejection publishes a toast error through the error bus", () => {
    const bus = createErrorBus();
    const target = new EventTarget();
    const cleanup = installRendererWindowErrorHandlers({ bus, target });

    target.dispatchEvent(eventWith("unhandledrejection", {
      reason: new Error("async boom"),
    }));

    const published = bus.getSnapshot().errors[0]!;
    expect(published.source).toBe("renderer.unhandled-rejection");
    expect(published.severity).toBe("error");
    expect(published.surface).toBe("toast");
    expect(published.envelope.type).toBe(
      "https://hoopoe.io/problems/renderer.unhandled-rejection",
    );
    expect(published.envelope.detail).toBe("async boom");

    cleanup();
  });

  test("error publishes a toast error through the error bus", () => {
    const bus = createErrorBus();
    const target = new EventTarget();
    const cleanup = installRendererWindowErrorHandlers({ bus, target });

    target.dispatchEvent(eventWith("error", {
      error: new Error("timer boom"),
      message: "fallback message",
    }));

    const published = bus.getSnapshot().errors[0]!;
    expect(published.source).toBe("renderer.window-error");
    expect(published.severity).toBe("error");
    expect(published.surface).toBe("toast");
    expect(published.envelope.type).toBe(
      "https://hoopoe.io/problems/renderer.window-error",
    );
    expect(published.envelope.detail).toBe("timer boom");

    cleanup();
  });

  test("cleanup removes both global listeners", () => {
    const bus = createErrorBus();
    const target = new EventTarget();
    const cleanup = installRendererWindowErrorHandlers({ bus, target });

    cleanup();
    target.dispatchEvent(eventWith("unhandledrejection", {
      reason: new Error("after cleanup"),
    }));
    target.dispatchEvent(eventWith("error", {
      error: new Error("after cleanup"),
    }));

    expect(bus.getSnapshot().errors).toHaveLength(0);
  });

  test("publish is best-effort when the bus throws", () => {
    expect(() => publishRendererWindowError("window-error", new Error("boom"), {
      dismiss: () => {},
      dismissAll: () => {},
      getSnapshot: () => ({ errors: [] }),
      publish: () => {
        throw new Error("bus down");
      },
      reset: () => {},
      subscribe: () => () => {},
    })).not.toThrow();
  });

  test("envelope builder uses manual toast actionability", () => {
    const envelope = buildRendererWindowErrorEnvelope(
      "unhandled-rejection",
      new Error("details"),
    );

    expect(envelope.surface).toBe("toast");
    expect(envelope.actionability).toBe("manual");
    expect(envelope.status).toBe(500);
    expect(envelope.detail).toBe("details");
  });
});

function eventWith(type: string, fields: Record<string, unknown>): Event {
  const event = new Event(type);
  for (const [key, value] of Object.entries(fields)) {
    Object.defineProperty(event, key, { value });
  }
  return event;
}
