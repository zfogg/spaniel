import { useEffect } from "react";
import { QueryClient, useQueryClient } from "@tanstack/react-query";
import { onWSEvent } from "./ws";

// Single shared client. This is a local dev tool talking to localhost, so we
// keep data briefly fresh and avoid focus/aggressive-retry refetch noise; live
// freshness comes from WebSocket-driven invalidation (useLiveInvalidation).
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

// Query-key registry. Keep keys here so pages and the live invalidator agree on
// the same prefixes. The first element of every key is the invalidation prefix
// used by useLiveInvalidation below.
export const qk = {
  traces: (p?: Record<string, unknown>) => ["traces", p ?? null] as const,
  trace: (id: string) => ["trace", id] as const,
  spans: (p?: Record<string, unknown>) => ["spans", p ?? null] as const,
  span: (id: string) => ["span", id] as const,
  logs: (p?: Record<string, unknown>) => ["logs", p ?? null] as const,
  metrics: (sessionId?: string) => ["metrics", sessionId ?? null] as const,
  metricSeries: (p: Record<string, unknown>) => ["metric-series", p] as const,
  serviceMap: (sessionId?: string) => ["service-map", sessionId ?? null] as const,
  coverage: (sessionId?: string) => ["coverage", sessionId ?? null] as const,
  sessions: () => ["sessions"] as const,
  session: (id: string) => ["session", id] as const,
  activeSession: () => ["active-session"] as const,
  issues: (key?: string) => ["issues", key ?? null] as const,
  lint: (sessionId?: string) => ["lint", sessionId ?? null] as const,
  stats: (sessionId?: string) => ["stats", sessionId ?? null] as const,
  services: () => ["services"] as const,
  forwarders: () => ["forwarders"] as const,
  sources: () => ["sources"] as const,
  storage: () => ["storage"] as const,
  settings: () => ["settings"] as const,
  dashboards: () => ["dashboards"] as const,
  alerts: (p?: Record<string, unknown>) => ["alerts", p ?? null] as const,
  notifications: () => ["notifications"] as const,
};

// Maps a live WebSocket event type to the query-key prefixes it should
// invalidate. Throughput is intentionally absent — BottomBar consumes it from
// the live stream directly, it is not query state.
const INVALIDATIONS: Record<string, string[]> = {
  // Dashboard panels execute read-only queries over these same live streams.
  // Invalidate their active observers from the event that made their results
  // stale, rather than polling the preview endpoint.
  span: [
    "traces",
    "trace",
    "spans",
    "span",
    "service-map",
    "stats",
    "lint",
    "coverage",
    "sessions",
    "dashboard-panel",
  ],
  log: ["logs", "dashboard-panel"],
  metric: ["metrics", "metric-series", "dashboard-panel"],
  issue: ["issues", "traces", "trace"],
  forwarder: ["forwarders"],
  alert: ["alerts", "notifications"],
};

// Opens a single WebSocket and turns live events into throttled query
// invalidations. Mount exactly once near the app root. Invalidations are
// coalesced (~1/sec per prefix) so a burst of spans does not trigger a refetch
// per message. invalidateQueries only refetches active observers, so marking a
// broad set of prefixes is cheap when those views aren't mounted.
export function useLiveInvalidation() {
  const qc = useQueryClient();
  useEffect(() => {
    const dirty = new Set<string>();
    let timer: ReturnType<typeof setTimeout> | null = null;

    const flush = () => {
      timer = null;
      for (const prefix of dirty) qc.invalidateQueries({ queryKey: [prefix] });
      dirty.clear();
    };

    const unsub = onWSEvent((ev) => {
      // Self-telemetry catalog frames update the Metrics sidebar locally. They
      // must not refetch telemetry queries, which would form a feedback loop.
      if (ev.type === "metric" && ev.payload.catalogOnly) return;
      const prefixes = INVALIDATIONS[ev.type];
      if (!prefixes) return;
      for (const p of prefixes) dirty.add(p);
      if (!timer) timer = setTimeout(flush, 1_000);
    });

    return () => {
      unsub();
      if (timer) clearTimeout(timer);
    };
  }, [qc]);
}
