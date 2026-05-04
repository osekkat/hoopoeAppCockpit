// hp-ndx5 — debounce primitive tests.

import { expect, test } from "bun:test";
import { createMockClock, debounce, realClock } from "./index.ts";

test("debounce: callback fires once after delayMs of quiet", () => {
  const clock = createMockClock();
  let fired = 0;
  const handle = debounce(() => { fired += 1; }, 100, clock);
  handle.trigger();
  expect(handle.pending()).toBe(true);
  clock.tick(99);
  expect(fired).toBe(0);
  clock.tick(1);
  expect(fired).toBe(1);
  expect(handle.pending()).toBe(false);
});

test("debounce: re-triggering resets the timer (collapse burst into one fire)", () => {
  const clock = createMockClock();
  let fired = 0;
  const handle = debounce(() => { fired += 1; }, 100, clock);
  handle.trigger();
  clock.tick(50);
  handle.trigger();
  clock.tick(50);
  // Still pending — second trigger reset the deadline.
  expect(fired).toBe(0);
  clock.tick(50);
  expect(fired).toBe(1);
});

test("debounce: cancel() prevents the callback from firing", () => {
  const clock = createMockClock();
  let fired = 0;
  const handle = debounce(() => { fired += 1; }, 100, clock);
  handle.trigger();
  handle.cancel();
  clock.tick(200);
  expect(fired).toBe(0);
  expect(handle.pending()).toBe(false);
});

test("debounce: 0ms delay is allowed; fires on the next tick", () => {
  const clock = createMockClock();
  let fired = 0;
  const handle = debounce(() => { fired += 1; }, 0, clock);
  handle.trigger();
  clock.tick(0);
  expect(fired).toBe(1);
});

test("debounce: refuses negative delayMs", () => {
  expect(() => debounce(() => undefined, -1)).toThrow(/delayMs must be >= 0/);
});

test("debounce: cancel() during callback is safe (no double-fire)", () => {
  const clock = createMockClock();
  let fired = 0;
  let handle: ReturnType<typeof debounce>;
  handle = debounce(() => {
    fired += 1;
    handle.cancel(); // should be a no-op since handle was already cleared
  }, 50, clock);
  handle.trigger();
  clock.tick(50);
  expect(fired).toBe(1);
});

test("createMockClock: pendingCount + reset", () => {
  const clock = createMockClock();
  clock.setTimeout(() => undefined, 10);
  clock.setTimeout(() => undefined, 20);
  expect(clock.pendingCount()).toBe(2);
  clock.reset();
  expect(clock.pendingCount()).toBe(0);
});

test("realClock: hooks into globalThis setTimeout/clearTimeout", async () => {
  // Smoke test against the real timer; should fire within a few ms.
  let fired = 0;
  const handle = debounce(() => { fired += 1; }, 5, realClock);
  handle.trigger();
  await new Promise((resolve) => setTimeout(resolve, 25));
  expect(fired).toBe(1);
});
