import { useEffect, useMemo, useState } from "react";
import { ariaLiveFor } from "./classification.ts";
import { errorBus as defaultBus } from "./errorBus.ts";
import { BannerStack } from "./surfaces/Banner.tsx";
import { BlockingModal } from "./surfaces/BlockingModal.tsx";
import { ToastStack } from "./surfaces/Toast.tsx";
import { recordTelemetry, type TelemetryConfig } from "./telemetry.ts";
import type { ErrorBus, PublishedError } from "./types.ts";

interface ErrorUxRootProps {
  readonly bus?: ErrorBus;
  readonly telemetry?: TelemetryConfig;
  readonly reducedMotion?: boolean;
}

/** Mounts under RootLayout. Subscribes to the bus and routes each
 *  active error to its surface. Pushes telemetry events into the
 *  configured sink (when opt-in is enabled). */
export function ErrorUxRoot({
  bus = defaultBus,
  telemetry,
  reducedMotion,
}: ErrorUxRootProps) {
  const [errors, setErrors] = useState<readonly PublishedError[]>(() =>
    bus.getSnapshot().errors,
  );
  const sentTelemetryIds = useMemo(() => new Set<string>(), []);
  const motionPreference = useMotionPreference(reducedMotion);

  useEffect(() => bus.subscribe(setErrors), [bus]);

  useEffect(() => {
    if (!telemetry) return;
    for (const error of errors) {
      if (sentTelemetryIds.has(error.id)) continue;
      recordTelemetry(error, telemetry);
      sentTelemetryIds.add(error.id);
    }
  }, [errors, telemetry, sentTelemetryIds]);

  const toasts = errors.filter((error) => error.surface === "toast");
  const banners = errors.filter((error) => error.surface === "banner");
  const modals = errors.filter((error) => error.surface === "blocking_modal");

  const liveAnnouncement =
    errors.length === 0 ? "" : formatAriaAnnouncement(errors[errors.length - 1]);
  const livePoliteness =
    errors.length === 0 ? "polite" : ariaLiveFor(errors[errors.length - 1]!.severity);

  return (
    <>
      <span
        className="hh-error-aria-live"
        role={livePoliteness === "assertive" ? "alert" : "status"}
        aria-live={livePoliteness}
        aria-atomic="true"
        data-testid="error-aria-live"
      >
        {liveAnnouncement}
      </span>
      <BannerStack bus={bus} errors={banners} />
      <ToastStack bus={bus} errors={toasts} reducedMotion={motionPreference} />
      <BlockingModal bus={bus} errors={modals} />
    </>
  );
}

function formatAriaAnnouncement(error: PublishedError | undefined): string {
  if (!error) return "";
  const severityLabel = `${error.severity.charAt(0).toUpperCase()}${error.severity.slice(1)}`;
  return `${severityLabel}: ${error.envelope.title}. ${error.envelope.user_message}`;
}

function useMotionPreference(explicit?: boolean): boolean {
  const [reduced, setReduced] = useState<boolean>(() => {
    if (explicit !== undefined) return explicit;
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return false;
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  });

  useEffect(() => {
    if (explicit !== undefined) return;
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return;
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const handler = (event: MediaQueryListEvent) => setReduced(event.matches);
    media.addEventListener("change", handler);
    return () => media.removeEventListener("change", handler);
  }, [explicit]);

  return reduced;
}
