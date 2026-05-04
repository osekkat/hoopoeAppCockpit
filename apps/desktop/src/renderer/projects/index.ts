// hp-ilt — public exports for `apps/desktop/src/renderer/projects/`.
//
// The picker route imports `ProjectEntry`; tests import the data hooks +
// validation helpers + readiness types directly.

export {
  ProjectEntry,
  ReadinessPanel,
  type ProjectEntryMode,
  type ProjectEntryProps,
} from "./ProjectEntry.tsx";

export {
  ProjectsBridgeUnavailableError,
  resolveDaemonRequest,
  useCloneProjectMutation,
  useCreateProjectMutation,
  useImportProjectMutation,
  useReadinessQuery,
  validateCloneInput,
  validateCreateInput,
  validateImportInput,
  type CloneValidation,
  type CreateValidation,
  type ImportValidation,
  type ProjectCloneInput,
  type ProjectCloneOutput,
  type ProjectCreateInput,
  type ProjectCreateOutput,
  type ProjectImportInput,
  type ProjectImportOutput,
  type ReadinessInput,
  type ReadinessOutput,
  type ReadinessRequirement,
} from "./data.ts";
