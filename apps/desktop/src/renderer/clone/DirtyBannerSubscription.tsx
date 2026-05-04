// hp-70yz — One-shot subscription effect for the dirty store.
//
// Renders nothing; subscribes to the events.clone.dirty IPC topic at
// mount and unsubscribes at unmount. Mounted once at the RootLayout
// level so there's exactly one subscription per app run.

import { useEffect } from "react";
import { subscribeToCloneDirtyEvents, useDirtyStore } from "./dirty-store.ts";

export function DirtyBannerSubscription() {
  const recordEvent = useDirtyStore((store) => store.recordEvent);
  useEffect(() => subscribeToCloneDirtyEvents(recordEvent), [recordEvent]);
  return null;
}
