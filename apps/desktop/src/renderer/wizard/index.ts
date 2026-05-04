// hp-o6q — public exports for the first-run wizard.

export {
  CHECKPOINT_OUTCOMES,
  FIRST_WIZARD_STEP_ID,
  WIZARD_PATHS,
  WIZARD_STATE_SCHEMA_VERSION,
  WIZARD_STEP_IDS,
  emptyWizardStateFile,
  type CheckpointEvent,
  type CheckpointFailure,
  type CheckpointOutcome,
  type WizardPath,
  type WizardRun,
  type WizardStateFile,
  type WizardStepId,
} from "./types.ts";

export {
  WizardReplaySink,
  WizardStateError,
  appendCheckpoint,
  fromStateFile,
  recordPath,
  startRun,
  toStateFile,
  type AppendCheckpointInput,
  type StartRunInput,
} from "./state.ts";

export {
  applicableStepsFor,
  canonicalNextStep,
  computeWizardState,
  lastCheckpointForStep,
  type WizardComputedState,
} from "./wizard-machine.ts";

export { WizardShell, type WizardShellProps } from "./WizardShell.tsx";
export { Step1PathPicker, type Step1PathPickerProps } from "./Step1PathPicker.tsx";
export { Step11Success, type Step11SuccessProps } from "./Step11Success.tsx";
export { StepStub, type StepStubProps } from "./StepStub.tsx";
export {
  StepAcfsInstall,
  StepBootstrapStream,
  StepPreflight,
  StepReconnect,
  StepVerifyKey,
  buildBootstrapCheckpointData,
  buildBootstrapFailure,
  invokeBootstrapStep,
  isBootstrapStepId,
  summarizeBootstrapEvents,
  type BootstrapDoctorCheck,
  type BootstrapEventSink,
  type BootstrapPhaseStatus,
  type BootstrapStepBridge,
  type BootstrapStepCheckpointData,
  type BootstrapStepFailure,
  type BootstrapStepId,
  type BootstrapStepResult,
  type BootstrapStepRunInput,
  type BootstrapStepSelection,
  type BootstrapStreamEvent,
} from "./StepBootstrapStream.tsx";
export {
  StepVpsConnect,
  type StepVpsConnectProps,
  type VpsConnectBridge,
  type VpsConnectFingerprintPrompt,
  type VpsConnectFormInput,
  type VpsConnectSelection,
} from "./StepVpsConnect.tsx";
