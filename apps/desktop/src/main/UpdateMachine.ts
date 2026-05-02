// Hoopoe-owned thin wrapper around vendored t3code update reducers. The
// reducer logic in vendored/t3code/updateMachine.ts is lifted whole; this
// module is the integration seam where Hoopoe's electron-updater event
// handlers (registered in hp-191) and the IpcRegistry will call into the
// reducers and broadcast resulting state via SettingsBridge's PubSub.

import {
  createInitialDesktopUpdateState,
  reduceDesktopUpdateStateOnCheckFailure,
  reduceDesktopUpdateStateOnCheckStart,
  reduceDesktopUpdateStateOnDownloadComplete,
  reduceDesktopUpdateStateOnDownloadFailure,
  reduceDesktopUpdateStateOnDownloadProgress,
  reduceDesktopUpdateStateOnDownloadStart,
  reduceDesktopUpdateStateOnInstallFailure,
  reduceDesktopUpdateStateOnNoUpdate,
  reduceDesktopUpdateStateOnUpdateAvailable,
} from "../vendored/t3code/updateMachine.ts";
import {
  shouldBroadcastDownloadProgress,
} from "../vendored/t3code/updateState.ts";
import type {
  DesktopRuntimeInfo,
  DesktopUpdateChannel,
  DesktopUpdateState,
} from "../vendored/t3code/_shims.ts";

export type {
  DesktopRuntimeInfo,
  DesktopUpdateChannel,
  DesktopUpdateState,
} from "../vendored/t3code/_shims.ts";

export {
  createInitialDesktopUpdateState,
  reduceDesktopUpdateStateOnCheckFailure,
  reduceDesktopUpdateStateOnCheckStart,
  reduceDesktopUpdateStateOnDownloadComplete,
  reduceDesktopUpdateStateOnDownloadFailure,
  reduceDesktopUpdateStateOnDownloadProgress,
  reduceDesktopUpdateStateOnDownloadStart,
  reduceDesktopUpdateStateOnInstallFailure,
  reduceDesktopUpdateStateOnNoUpdate,
  reduceDesktopUpdateStateOnUpdateAvailable,
  shouldBroadcastDownloadProgress,
};

export interface UpdateMachine {
  readonly current: () => DesktopUpdateState;
  readonly onCheckStart: (checkedAt: string) => DesktopUpdateState;
  readonly onCheckFailure: (message: string, checkedAt: string) => DesktopUpdateState;
  readonly onUpdateAvailable: (version: string, checkedAt: string) => DesktopUpdateState;
  readonly onNoUpdate: (checkedAt: string) => DesktopUpdateState;
  readonly onDownloadStart: () => DesktopUpdateState;
  readonly onDownloadProgress: (percent: number) => DesktopUpdateState;
  readonly onDownloadFailure: (message: string) => DesktopUpdateState;
  readonly onDownloadComplete: (version: string) => DesktopUpdateState;
  readonly onInstallFailure: (message: string) => DesktopUpdateState;
}

export function createUpdateMachine(input: {
  readonly currentVersion: string;
  readonly runtimeInfo: DesktopRuntimeInfo;
  readonly channel: DesktopUpdateChannel;
}): UpdateMachine {
  let state = createInitialDesktopUpdateState(
    input.currentVersion,
    input.runtimeInfo,
    input.channel,
  );
  const apply = <Args extends unknown[]>(
    fn: (state: DesktopUpdateState, ...args: Args) => DesktopUpdateState,
    ...args: Args
  ): DesktopUpdateState => {
    state = fn(state, ...args);
    return state;
  };

  return {
    current: () => state,
    onCheckStart: (checkedAt) => apply(reduceDesktopUpdateStateOnCheckStart, checkedAt),
    onCheckFailure: (message, checkedAt) =>
      apply(reduceDesktopUpdateStateOnCheckFailure, message, checkedAt),
    onUpdateAvailable: (version, checkedAt) =>
      apply(reduceDesktopUpdateStateOnUpdateAvailable, version, checkedAt),
    onNoUpdate: (checkedAt) => apply(reduceDesktopUpdateStateOnNoUpdate, checkedAt),
    onDownloadStart: () => apply(reduceDesktopUpdateStateOnDownloadStart),
    onDownloadProgress: (percent) =>
      apply(reduceDesktopUpdateStateOnDownloadProgress, percent),
    onDownloadFailure: (message) => apply(reduceDesktopUpdateStateOnDownloadFailure, message),
    onDownloadComplete: (version) => apply(reduceDesktopUpdateStateOnDownloadComplete, version),
    onInstallFailure: (message) => apply(reduceDesktopUpdateStateOnInstallFailure, message),
  };
}
