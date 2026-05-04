// hp-m79e — One-shot subscription effect for the tunnel store.
//
// Renders nothing; on mount:
//   1. Hydrates the store from the daemon's `tunnel.snapshot` RPC.
//   2. Subscribes to `events.tunnel` for live transitions.
// On unmount, the subscribe handle is released. Mount once at the
// RootLayout level so the cockpit has exactly one tunnel subscription
// per app run.

import { useEffect } from "react";
import {
  hydrateFromBridge,
  subscribeToTunnelEvents,
  useTunnelStore,
} from "./tunnel-store.ts";

export function TunnelSubscription() {
  const recordEvent = useTunnelStore((store) => store.recordEvent);
  useEffect(() => {
    void hydrateFromBridge(recordEvent);
    const unsubscribe = subscribeToTunnelEvents(recordEvent);
    return unsubscribe;
  }, [recordEvent]);
  return null;
}
