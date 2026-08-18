import { type ReactNode, useCallback, useEffect, useRef } from "react"
import { useReferenceHover, type ReferenceLevel } from "@/hooks/reference-context"
import { useHoverPopover } from "@/hooks/useHoverPopover"
import { cn } from "@/lib/utils"

// ReferenceTrigger wraps a piece of table-cell text (an IP, a domain, an
// Address Object name, an Address Object entry) and turns it into a
// reference-popover trigger, WITHOUT rendering its own Popover Root — it
// only calls into ReferenceHoverProvider's context on hover/focus intent
// (docs/ref/todo/reference-popover-plan.md Step 8, plan §5 Caution 2).
//
// `level` selects the open delay: level 1 (a table cell, the default) opens
// after 1000ms; level 2 (an entry inside an already-open level-1 popover,
// e.g. Step 11's AddressObjectReferenceContent) opens after a shorter
// ~300ms, since the user has already shown intent by opening level 1.
export interface ReferenceTriggerProps {
  level?: ReferenceLevel
  /** The popover's content — computed lazily, only once hover/focus intent
   * actually fires (never up front for every row), so a level-2 entry's
   * `content()` (which may trigger a network request via referenceService)
   * only runs when the user actually hovers it (plan Step 11: "popover
   * ระดับ 1 ต้องไม่มี network request เลย"). */
  content: () => ReactNode
  children: ReactNode
  className?: string
}

export function ReferenceTrigger({ level = 1, content, children, className }: ReferenceTriggerProps) {
  const hover = useReferenceHover()
  const targetRef = useRef<HTMLElement | null>(null)
  const contentRef = useRef(content)
  useEffect(() => {
    contentRef.current = content
  })

  const openDelayMs = level === 1 ? 1000 : 300

  const doOpen = useCallback(() => {
    const target = targetRef.current
    if (!hover || !target) return
    if (level === 1) {
      hover.openLevel1(target, contentRef.current())
    } else {
      hover.openLevel2(target, contentRef.current())
    }
  }, [hover, level])

  const {
    scheduleOpen,
    // scheduleClose here only ever cancels THIS hook's own pending open
    // timer (its onClose is a no-op above) — it is never what actually
    // closes the provider's popover, that's scheduleCloseForLevel below.
    scheduleClose: cancelPendingOpen,
    openNow,
  } = useHoverPopover({
    openDelayMs,
    onOpen: doOpen,
    onClose: () => {
      // Closing is owned by ReferenceHoverProvider's own close-grace timer
      // (scheduleCloseLevel{1,2} below IS that timer) — this hook instance
      // is only used here to drive the OPEN half of the hover-intent
      // machine; onClose is intentionally unused.
    },
  })

  if (!hover) {
    // No provider mounted (a call site not yet wired up, or a test) —
    // degrade to a plain, fully inert span so nothing crashes and nothing
    // looks interactive.
    return <span className={className}>{children}</span>
  }

  const scheduleCloseForLevel = level === 1 ? hover.scheduleCloseLevel1 : hover.scheduleCloseLevel2
  const cancelCloseForLevel = level === 1 ? hover.cancelCloseLevel1 : hover.cancelCloseLevel2

  return (
    <span
      className={cn("cursor-default", className)}
      tabIndex={0}
      onPointerEnter={(e) => {
        if (e.pointerType === "touch") return // plan Step 7: touch must never open on hover
        targetRef.current = e.currentTarget
        cancelCloseForLevel()
        scheduleOpen()
      }}
      onPointerLeave={(e) => {
        if (e.pointerType === "touch") return
        cancelPendingOpen() // cancel this trigger's own not-yet-fired open timer
        scheduleCloseForLevel() // and, if already open, start the provider's close-grace timer
      }}
      onFocus={(e) => {
        targetRef.current = e.currentTarget
        cancelCloseForLevel()
        openNow() // plan Step 7: focus opens immediately, no delay
      }}
      onBlur={() => {
        cancelPendingOpen()
        scheduleCloseForLevel()
      }}
      // No onKeyDown/Escape handler here on purpose: each Popover Root in
      // ReferenceHoverProvider already gets Radix's own per-layer
      // DismissableLayer Escape-dismiss (its onOpenChange(false) closes just
      // that ONE level). A trigger-level `hover.closeAll()` would fire in
      // the SAME event as that per-layer dismiss whenever the trigger still
      // has focus (e.g. after openNow() via keyboard), closing both levels
      // at once instead of one at a time (plan Step 8/Definition of Done:
      // "Escape ปิดทีละระดับ").
    >
      {children}
    </span>
  )
}
