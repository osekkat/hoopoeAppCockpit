// `apps/desktop/src/test-utils/chaos` — chaos / fault-injection primitives (hp-2qn).
//
// Tests under `apps/desktop/tests/integration/chaos/` import from
// here so the call sites describe their intent ("freeze the daemon
// for 2s") without re-implementing signal/sleep dances per test.
// Primitives operate on the `DaemonHandle` from
// `apps/desktop/src/test-utils/daemon-harness/index.ts` (hp-ngq) so
// chaos tests can ride on the same harness lifecycle.

export {
  ChaosProcessError,
  pauseProcess,
  resumeProcess,
  shutdownProcess,
  withPaused,
  type ProcessTarget,
  type RestartOptions,
} from "./process-control.ts";

export {
  slowConsumeReadable,
  slowConsumeWebSocketMessages,
  type SlowConsumeOptions,
} from "./slow-consumer.ts";

export {
  ChaosDiskPressureError,
  fillDisk,
  type DiskPressureHandle,
  type DiskPressureOptions,
} from "./disk-pressure.ts";

export {
  ChaosMalformedAdapterError,
  loadMalformedAdapterFixture,
  parseMalformedFixture,
  type MalformedAdapterFixture,
  type MalformedAdapterOptions,
} from "./malformed-adapter.ts";
