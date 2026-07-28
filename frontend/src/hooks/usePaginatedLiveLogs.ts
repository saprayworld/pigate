import { useCallback, useEffect, useRef, useState } from "react"

import { dashboardService, type SSELogEntry } from "@/services/dashboardService"

/**
 * Cursor-based infinite-scroll log feed shared by the Forward Traffic and
 * Local Traffic pages (docs/ref/todo/traffic-log-pagination-and-local-traffic-plan.md
 * §2.7/§2.8). Built on top of the same SSE stream as useLiveLogs, but unlike
 * that hook's "replace on snapshot" behavior, this one:
 *
 * - fetches the first page (pageSize rows) on mount/refreshKey change, discarding
 *   any previously accumulated pages and the cursor (a filter change must start over);
 * - exposes `loadMore()` to fetch the next page using the LAST row currently in
 *   the list as the (id, time) cursor, appended + deduped by id;
 * - on SSE `onLog`, PREPENDS the new row (deduped) WITHOUT touching the cursor —
 *   a live push must never affect "where infinite scroll currently is";
 * - on SSE `onOpen`/`onError` (reconnect), refetches only the FIRST page and
 *   merges it into the HEAD of the list (dedupe by id) — it must never replace
 *   the whole list, or pages a user already scrolled through would vanish
 *   (Caution 6/§2.7 "merge กับ SSE");
 * - on `onClear`, empties the list and resets pagination to "start over, more
 *   data available";
 * - enforces `maxRows` by trimming the TAIL (oldest) of the list when the cap
 *   is exceeded, and forces `hasMore = true` in that case since the cursor is
 *   always derived from whatever row is currently last (Caution 9).
 */
export interface UsePaginatedLiveLogsOptions<T> {
  /** Fetches one page, newest-first, honoring the given cursor (undefined = first page). */
  fetchPage: (cursor?: { beforeId: string; beforeTime: string }) => Promise<T[]>
  /** Change to force a full reset + first-page refetch (filter change). */
  refreshKey?: number | string
  /** When true the SSE stream is closed entirely (Pause button). */
  paused?: boolean
  /** Map an incoming SSE entry to T, or return null to drop it (client-side
   *  filter mirror — must match the server filter, see plan §6 Caution 6). */
  transform?: (raw: SSELogEntry) => T | null
  /** Rows requested per page (also the "next page" size for loadMore). */
  pageSize?: number
  /** Hard cap on rows kept in memory; oldest rows are trimmed past this. */
  maxRows?: number
  /** Extracts (id, time) from a row, for building the next cursor. Defaults
   *  to reading `.id`/`.time` directly. */
  getCursorFields?: (row: T) => { id: string; time: string }
}

export interface UsePaginatedLiveLogsResult<T> {
  logs: T[]
  isLoading: boolean
  isLoadingMore: boolean
  hasMore: boolean
  loadMore: () => void
}

const DEFAULT_PAGE_SIZE = 500
const DEFAULT_MAX_ROWS = 5000

export function usePaginatedLiveLogs<T extends { id: string; time?: string }>({
  fetchPage,
  refreshKey = 0,
  paused = false,
  transform,
  pageSize = DEFAULT_PAGE_SIZE,
  maxRows = DEFAULT_MAX_ROWS,
  getCursorFields,
}: UsePaginatedLiveLogsOptions<T>): UsePaginatedLiveLogsResult<T> {
  const [logs, setLogs] = useState<T[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(true)

  const fetchRef = useRef(fetchPage)
  const transformRef = useRef(transform)
  const cursorFieldsRef = useRef(getCursorFields)
  useEffect(() => {
    fetchRef.current = fetchPage
    transformRef.current = transform
    cursorFieldsRef.current = getCursorFields
  })

  // Guards against overlapping loadMore calls (e.g. IntersectionObserver
  // firing multiple times before the previous fetch resolves).
  const loadingMoreRef = useRef(false)

  const cursorOf = useCallback(
    (row: T): { id: string; time: string } => {
      if (cursorFieldsRef.current) return cursorFieldsRef.current(row)
      return { id: row.id, time: (row as { time?: string }).time ?? "" }
    },
    []
  )

  // First-page load: discards accumulated pages + cursor entirely.
  const loadFirstPage = useCallback(async () => {
    setIsLoading(true)
    setHasMore(true)
    try {
      const page = await fetchRef.current()
      setLogs(page.slice(0, maxRows))
      setHasMore(page.length >= pageSize)
    } catch {
      /* keep last known rows — a transient failure shouldn't blank the view */
    } finally {
      setIsLoading(false)
    }
  }, [maxRows, pageSize])

  // Indirection through a ref (matching the pattern used elsewhere in this
  // codebase, e.g. EventLogs.tsx) so the effect below calls the latest
  // loadFirstPage without needing it in its dependency array.
  const loadFirstPageRef = useRef(loadFirstPage)
  useEffect(() => {
    loadFirstPageRef.current = loadFirstPage
  })

  // Reset + fetch first page whenever refreshKey changes (filter change) or on mount.
  useEffect(() => {
    loadFirstPageRef.current()
  }, [refreshKey])

  const loadMore = useCallback(() => {
    if (loadingMoreRef.current || !hasMore) return
    setLogs((currentLogs) => {
      if (currentLogs.length === 0) return currentLogs
      const last = currentLogs[currentLogs.length - 1]
      const { id: beforeId, time: beforeTime } = cursorOf(last)

      loadingMoreRef.current = true
      setIsLoadingMore(true)
      fetchRef
        .current({ beforeId, beforeTime })
        .then((nextPage) => {
          setLogs((prev) => {
            const seen = new Set(prev.map((l) => l.id))
            const appended = nextPage.filter((l) => !seen.has(l.id))
            const merged = [...prev, ...appended]
            return merged.length > maxRows ? merged.slice(0, maxRows) : merged
          })
          setHasMore(nextPage.length >= pageSize)
        })
        .catch(() => {
          /* transient failure: leave hasMore as-is so the user can retry */
        })
        .finally(() => {
          loadingMoreRef.current = false
          setIsLoadingMore(false)
        })
      return currentLogs
    })
  }, [hasMore, maxRows, pageSize, cursorOf])

  // The live SSE stream. Only re-runs when paused toggles.
  useEffect(() => {
    if (paused) return
    const stop = dashboardService.connectSSELogs({
      // (Re)connected — refetch ONLY the first page and merge into the head;
      // never replace the whole list (would drop pages already scrolled to).
      onOpen: () => {
        fetchRef
          .current()
          .then((firstPage) => {
            setLogs((prev) => {
              const seen = new Set(prev.map((l) => l.id))
              const newOnes = firstPage.filter((l) => !seen.has(l.id))
              if (newOnes.length === 0) return prev
              const merged = [...newOnes, ...prev]
              return merged.length > maxRows ? merged.slice(0, maxRows) : merged
            })
          })
          .catch(() => {
            /* keep current rows on a transient refetch failure */
          })
      },
      onLog: (raw) => {
        const apply = transformRef.current
        const entry = apply ? apply(raw) : (raw as unknown as T)
        if (!entry) return
        setLogs((prev) => {
          if (prev.some((l) => l.id === entry.id)) return prev
          const merged = [entry, ...prev]
          return merged.length > maxRows ? merged.slice(0, maxRows) : merged
        })
      },
      onClear: () => {
        setLogs([])
        setHasMore(true)
      },
      onError: () => {
        fetchRef
          .current()
          .then((firstPage) => {
            setLogs((prev) => {
              const seen = new Set(prev.map((l) => l.id))
              const newOnes = firstPage.filter((l) => !seen.has(l.id))
              if (newOnes.length === 0) return prev
              const merged = [...newOnes, ...prev]
              return merged.length > maxRows ? merged.slice(0, maxRows) : merged
            })
          })
          .catch(() => {
            /* keep current rows on a transient refetch failure */
          })
      },
    })
    return stop
  }, [paused, maxRows])

  return { logs, isLoading, isLoadingMore, hasMore, loadMore }
}
