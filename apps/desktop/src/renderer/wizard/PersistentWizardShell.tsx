// hp-zsp1 - Settings-backed first-run wizard shell.
//
// WizardShell remains a pure/replayable component. This wrapper is the
// production renderer bridge: load persisted checkpoints from
// window.hoopoe.settings.get(), replay them into WizardReplaySink, and write
// each new checkpoint back through window.hoopoe.settings.set().

import { useCallback, useEffect, useMemo, useState } from "react";
import { Loader2, TriangleAlert } from "lucide-react";
import { WizardShell, type WizardShellProps } from "./WizardShell.tsx";
import { WizardReplaySink, fromStateFile, type WizardStateError } from "./state.ts";
import type { WizardRun, WizardStateFile } from "./types.ts";

export interface WizardSettingsBridge {
  readonly get: <T>() => Promise<T>;
  readonly set: <T>(partial: T) => Promise<void>;
}

export interface PersistentWizardShellProps extends Omit<WizardShellProps, "persist" | "sink"> {
  readonly settingsBridge?: WizardSettingsBridge | null;
}

interface WizardSettingsTree {
  readonly wizard?: unknown;
}

interface WizardSettingsPatch {
  readonly wizard: WizardStateFile;
}

export function PersistentWizardShell({
  settingsBridge,
  ...shellProps
}: PersistentWizardShellProps) {
  const bridge = settingsBridge === undefined ? defaultWizardSettingsBridge() : settingsBridge;
  const [sink, setSink] = useState<WizardReplaySink | null>(() =>
    bridge ? null : new WizardReplaySink(),
  );
  const [loadError, setLoadError] = useState<string | null>(null);
  const [persistError, setPersistError] = useState<string | null>(null);

  useEffect(() => {
    if (!bridge) {
      setSink(new WizardReplaySink());
      setLoadError(null);
      return;
    }
    let cancelled = false;
    setSink(null);
    setLoadError(null);
    void bridge
      .get<WizardSettingsTree>()
      .then((settings) => {
        if (cancelled) return;
        setSink(new WizardReplaySink(wizardRunsFromSettings(settings)));
      })
      .catch((err) => {
        if (cancelled) return;
        setLoadError(err instanceof Error ? err.message : String(err));
        setSink(new WizardReplaySink());
      });
    return () => {
      cancelled = true;
    };
  }, [bridge]);

  const persist = useCallback(
    (_run: WizardRun) => {
      if (!bridge || !sink) return;
      setPersistError(null);
      void bridge.set<WizardSettingsPatch>(buildWizardSettingsPatch(sink)).catch((err) => {
        setPersistError(err instanceof Error ? err.message : String(err));
      });
    },
    [bridge, sink],
  );

  const body = useMemo(() => {
    if (!sink) {
      return (
        <section className="hh-wizard" data-testid="wizard-persisted-loading">
          <div className="hh-wizard-persisted-state">
            <Loader2 className="hh-spin" size={18} strokeWidth={2.1} />
            <div>
              <strong>Loading wizard checkpoints</strong>
              <p>Reading client settings before resuming the first-run flow.</p>
            </div>
          </div>
        </section>
      );
    }
    return <WizardShell {...shellProps} sink={sink} persist={persist} />;
  }, [persist, shellProps, sink]);

  return (
    <>
      {body}
      {loadError || persistError ? (
        <div className="hh-wizard-persisted-warning" data-testid="wizard-persisted-warning" role="alert">
          <TriangleAlert size={16} strokeWidth={2.1} />
          <div>
            <strong>Wizard checkpoint persistence warning</strong>
            <p>{persistError ?? loadError}</p>
          </div>
        </div>
      ) : null}
    </>
  );
}

export function wizardRunsFromSettings(settings: unknown): readonly WizardRun[] {
  if (settings === null || typeof settings !== "object") return [];
  const wizard = (settings as WizardSettingsTree).wizard;
  if (wizard === undefined || wizard === null) return [];
  return fromStateFile(wizard);
}

export function buildWizardSettingsPatch(sink: WizardReplaySink): WizardSettingsPatch {
  return { wizard: sink.toFile() };
}

interface HoopoeSettingsBridgeShape {
  readonly settings?: WizardSettingsBridge;
}

function defaultWizardSettingsBridge(): WizardSettingsBridge | null {
  if (typeof window === "undefined") return null;
  const settings = (window as Window & { readonly hoopoe?: HoopoeSettingsBridgeShape }).hoopoe?.settings;
  if (!settings) return null;
  return settings;
}

export type { WizardStateError };
