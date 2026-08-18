import { useCallback, useEffect, useRef } from "react"

// useHoverPopover — the generic hover-intent timer machine backing the
// reference popover (docs/ref/todo/reference-popover-plan.md Step 7): a
// level-agnostic "open after a delay, close after a shorter grace period,
// cancel the close if re-entered" state machine. It knows nothing about
// Popover/anchors/content — ReferenceTrigger (Step 8) wires it to
// ReferenceHoverContext's openLevel/closeLevel functions. Level 1 (a table
// cell) uses the 1000ms open delay; level 2 (an entry inside an already-open
// popover) uses a shorter ~300ms one — both are configurable per the plan's
// two different rows in the Definition of Done checklist.

export interface UseHoverPopoverOptions {
  /** Delay before onOpen fires from scheduleOpen(). Default 1000ms (plan
   * Step 7 — level 1's hover delay). */
  openDelayMs?: number
  /** Grace period before onClose fires from scheduleClose() — long enough for
   * the mouse to travel from the trigger into the popover content (the
   * "hover-bridge") without it closing. Default ~200ms. */
  closeDelayMs?: number
  onOpen: () => void
  onClose: () => void
}

export interface UseHoverPopoverResult {
  /** Start (or restart) the open-delay timer, cancelling any pending close. */
  scheduleOpen: (delayMs?: number) => void
  /** Start the close-grace timer, cancelling any pending open. */
  scheduleClose: () => void
  /** Cancel a pending close — call from the popover content's own
   * onMouseEnter (the "hover-bridge": entering the content before the close
   * grace period elapses keeps it open). */
  cancelClose: () => void
  /** Open immediately, no delay — used for keyboard focus (plan Step 7:
   * "เปิดด้วย focus ไม่ต้องรอ delay"). */
  openNow: () => void
  /** Close immediately, no grace period — used for Escape/scroll/touch. */
  closeNow: () => void
}

export function useHoverPopover({
  openDelayMs = 1000,
  closeDelayMs = 200,
  onOpen,
  onClose,
}: UseHoverPopoverOptions): UseHoverPopoverResult {
  const openTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  // onOpen/onClose are read through refs so scheduleOpen/scheduleClose below
  // can stay referentially stable across renders without forcing every
  // caller to useCallback their handlers. Synced in an effect (never during
  // render, same convention as useLiveLogs.ts's fetchRef/transformRef).
  const onOpenRef = useRef(onOpen)
  const onCloseRef = useRef(onClose)
  useEffect(() => {
    onOpenRef.current = onOpen
    onCloseRef.current = onClose
  })

  const clearOpenTimer = () => {
    if (openTimer.current !== null) {
      clearTimeout(openTimer.current)
      openTimer.current = null
    }
  }
  const clearCloseTimer = () => {
    if (closeTimer.current !== null) {
      clearTimeout(closeTimer.current)
      closeTimer.current = null
    }
  }

  const scheduleOpen = useCallback(
    (delayMs?: number) => {
      clearCloseTimer()
      clearOpenTimer()
      openTimer.current = setTimeout(() => {
        openTimer.current = null
        onOpenRef.current()
      }, delayMs ?? openDelayMs)
    },
    [openDelayMs]
  )

  const scheduleClose = useCallback(() => {
    clearOpenTimer()
    clearCloseTimer()
    closeTimer.current = setTimeout(() => {
      closeTimer.current = null
      onCloseRef.current()
    }, closeDelayMs)
  }, [closeDelayMs])

  const cancelClose = useCallback(() => {
    clearCloseTimer()
  }, [])

  const openNow = useCallback(() => {
    clearOpenTimer()
    clearCloseTimer()
    onOpenRef.current()
  }, [])

  const closeNow = useCallback(() => {
    clearOpenTimer()
    clearCloseTimer()
    onCloseRef.current()
  }, [])

  // Clear every pending timer on unmount — a trigger that hovers-in then
  // unmounts before its delay elapses (e.g. a filtered-out table row) must
  // never fire onOpen/onClose against a dead component (plan Step 7:
  // "clear timer ทุกตัวตอน unmount").
  useEffect(() => {
    return () => {
      clearOpenTimer()
      clearCloseTimer()
    }
  }, [])

  return { scheduleOpen, scheduleClose, cancelClose, openNow, closeNow }
}
