import { create } from "zustand";
import {
  createJSONStorage,
  persist,
  type StateStorage,
} from "zustand/middleware";
import type { ShellRouteId } from "./stages.ts";

export interface ShellUiState {
  readonly activityPanelOpen: boolean;
  readonly lastProjectId: string | null;
  readonly lastStageId: ShellRouteId;
  readonly setActivityPanelOpen: (open: boolean) => void;
  readonly toggleActivityPanel: () => void;
  readonly rememberProject: (projectId: string) => void;
  readonly rememberStage: (stageId: ShellRouteId) => void;
}

const memoryStorage = new Map<string, string>();

const fallbackStorage: StateStorage = {
  getItem: (name) => memoryStorage.get(name) ?? null,
  setItem: (name, value) => {
    memoryStorage.set(name, value);
  },
  removeItem: (name) => {
    memoryStorage.delete(name);
  },
};

export const useShellUiStore = create<ShellUiState>()(
  persist(
    (set) => ({
      activityPanelOpen: false,
      lastProjectId: null,
      lastStageId: "plan",
      setActivityPanelOpen: (open) => {
        set({ activityPanelOpen: open });
      },
      toggleActivityPanel: () => {
        set((state) => ({ activityPanelOpen: !state.activityPanelOpen }));
      },
      rememberProject: (projectId) => {
        set({ lastProjectId: projectId });
      },
      rememberStage: (stageId) => {
        set({ lastStageId: stageId });
      },
    }),
    {
      name: "hoopoe.desktop.shell.v1",
      storage: createJSONStorage(() =>
        typeof window === "undefined" ? fallbackStorage : window.localStorage,
      ),
      partialize: (state) => ({
        activityPanelOpen: state.activityPanelOpen,
        lastProjectId: state.lastProjectId,
        lastStageId: state.lastStageId,
      }),
    },
  ),
);
