import { useCallback, useEffect, useRef, useState } from "react"

import { dashboardService, type SSELogEntry } from "@/services/dashboardService"

// Cap the in-memory list at the backend ring-buffer capacity (main.go:74) so a
// long-lived stream can't grow the array without bound.
const MAX_LOGS = 500

interface UseLiveLogsOptions<T> {
  /** Fetches the authoritative newest-first snapshot (server-filtered). Called
   *  on mount, on every (re)connect, on error, and when refreshKey changes. */
  fetchSnapshot: () => Promise<T[]>
  /** Change to force a snapshot refetch (Dashboard Refresh button / ForwardTraffic
   *  filter change). A number counter or a string key derived from the filters. */
  refreshKey?: number | string
  /** When true the SSE stream is closed entirely (ForwardTraffic Pause). */
  paused?: boolean
  /** Map an incoming SSE entry to T, or return null to drop it — this is where
   *  ForwardTraffic applies its client-side filter so filtered rows never leak
   *  in via the live stream. Defaults to an identity cast (Dashboard). */
  transform?: (raw: SSELogEntry) => T | null
}

/**
 * Live log feed shared by the Dashboard Recent Logs widget and the Forward
 * Traffic page. Replaces interval polling with the SSE push stream:
 *
 * - a snapshot fetch seeds/reseeds the list (mount, reconnect, error, refresh);
 *   the server view is authoritative, which fills any gap left by a dropped
 *   connection (Caution 6);
 * - each pushed entry prepends, deduped by `id`, so an entry already present in
 *   a snapshot is never doubled when its live event also arrives (Caution 6);
 * - a `clear` event empties the list so every open browser follows a Clear from
 *   any one of them;
 * - `paused` closes the stream; resuming reconnects and the `open` handler
 *   refetches to catch up.
 *
 * Note the tiny race where an entry pushed while a snapshot is in flight can be
 * momentarily dropped by the replacing snapshot; it self-heals on the next
 * snapshot (refresh/reconnect) since it remains in the server buffer.
 */
export function useLiveLogs<T extends { id: string }>({
  fetchSnapshot,
  refreshKey = 0,
  paused = false,
  transform,
}: UseLiveLogsOptions<T>): { logs: T[]; isLoading: boolean } {
  const [logs, setLogs] = useState<T[]>([])
  const [isLoading, setIsLoading] = useState(true)

  // Hold the latest fetch/transform in refs so the SSE effect never reconnects
  // just because these got a new identity on render (ForwardTraffic rebuilds
  // both whenever a filter changes).
  const fetchRef = useRef(fetchSnapshot)
  const transformRef = useRef(transform)
  useEffect(() => {
    fetchRef.current = fetchSnapshot
    transformRef.current = transform
  })

  // Fetch the snapshot and replace the list. Never sets loading synchronously
  // (isLoading starts true and drops to false once the first fetch settles); a
  // filter/refresh keeps the current rows visible rather than flashing a spinner,
  // matching the Dashboard usePoll behavior.
  const loadSnapshot = useCallback(async () => {
    try {
      const data = await fetchRef.current()
      setLogs(data.slice(0, MAX_LOGS))
    } catch {
      /* keep last known rows — a transient failure shouldn't blank the view */
    } finally {
      setIsLoading(false)
    }
  }, [])

  // Snapshot on mount and whenever refreshKey changes. Independent of the SSE
  // connection so a filter/refresh never tears the stream down.
  useEffect(() => {
    void loadSnapshot()
  }, [refreshKey, loadSnapshot])

  // The live SSE stream. Only re-runs when paused toggles.
  useEffect(() => {
    if (paused) return
    const stop = dashboardService.connectSSELogs({
      // (Re)connected — refetch to close any gap accumulated while disconnected.
      onOpen: () => {
        void loadSnapshot()
      },
      onLog: (raw) => {
        const apply = transformRef.current
        const entry = apply ? apply(raw) : (raw as unknown as T)
        if (!entry) return
        setLogs((prev) => {
          if (prev.some((l) => l.id === entry.id)) return prev
          return [entry, ...prev].slice(0, MAX_LOGS)
        })
      },
      onClear: () => setLogs([]),
      // EventSource hides the HTTP status, so a 401 on reconnect is invisible
      // here; refetching routes that failure through the fetch wrapper's 401 ->
      // /login redirect (config.ts). It also just catches up after a blip.
      onError: () => {
        void loadSnapshot()
      },
    })
    return stop
  }, [paused, loadSnapshot])

  return { logs, isLoading }
}
