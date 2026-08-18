import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover"
import { ReferenceHoverContext, type ReferenceHoverContextValue } from "@/hooks/reference-context"
import { useHoverPopover } from "@/hooks/useHoverPopover"

// zeroRectAnchor is the virtualRef fallback before any trigger has opened a
// popover yet — Radix Popper's Measurable type has no "null" case, so the
// ref must always point at SOMETHING with a getBoundingClientRect(); this
// never actually renders (the PopoverContent below is only mounted while
// level1/level2 state is non-null).
const zeroRectAnchor = {
  getBoundingClientRect: () =>
    ({ x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, toJSON: () => ({}) }) as DOMRect,
}

// ReferenceHoverProvider — the SINGLE pair of Radix Popover Roots backing
// the reference popover on a whole page (docs/ref/todo/
// reference-popover-plan.md Step 8/§5 Caution 2): a table with up to 5,000
// rows (TrafficLogPage.tsx's MAX_ROWS) must never mount one Popover Root per
// cell, so every ReferenceTrigger on the page calls into this single
// provider's context instead of rendering its own <Popover>. Wrap a page
// (or a Drawer's content, which is its own portal — see RuleStatsDrawer.tsx)
// once with <ReferenceHoverProvider>...</ReferenceHoverProvider>.
//
// Exactly 2 stacked levels, both anchored via Radix Popper's `virtualRef`
// pointing straight at the hovered DOM element (no manual
// getBoundingClientRect() bookkeeping needed — an HTMLElement already
// satisfies Popper's Measurable interface).
//
// `closeWhen` (optional) lets a caller force both levels closed immediately
// without needing its own descendant component to reach into the context
// (plan §5 Caution 3 — PolicyChainPage.tsx must close every open popover the
// instant its Drawer/Dialog opens, so it can't collide with the Combobox's
// focus/pointer handling): pass e.g. `isModalOpen || isStatsDrawerOpen`.
// Deliberately read directly in the `open` prop below (a derived render
// value), never synced into state via a setState-in-effect — closeLevel*Now
// still clears the underlying level1/level2 state on the next real
// open/close, `closeWhen` just forces the Popover itself shut in the
// meantime.
export function ReferenceHoverProvider({ children, closeWhen }: { children: ReactNode; closeWhen?: boolean }) {
  const [level1, setLevel1] = useState<{ content: ReactNode } | null>(null)
  const [level2, setLevel2] = useState<{ content: ReactNode } | null>(null)
  const anchor1Ref = useRef<{ getBoundingClientRect: () => DOMRect }>(zeroRectAnchor)
  const anchor2Ref = useRef<{ getBoundingClientRect: () => DOMRect }>(zeroRectAnchor)

  const closeLevel2Now = useCallback(() => setLevel2(null), [])
  const closeLevel1Now = useCallback(() => {
    setLevel1(null)
    // Level 2 is always logically "inside" level 1 (an entry drilled into
    // from an already-open level-1 popover) — closing level 1 must always
    // close level 2 too, never leave it orphaned/floating with no visible
    // parent.
    setLevel2(null)
  }, [])

  const level1Hover = useHoverPopover({
    // Not used to OPEN (ReferenceTrigger instances own their own open-delay
    // timer before ever calling openLevel1) — only its close-grace/cancel
    // machinery is used here, for the hover-bridge between the trigger and
    // this popover's own content.
    openDelayMs: 0,
    closeDelayMs: 200,
    onOpen: () => {},
    onClose: closeLevel1Now,
  })
  const level2Hover = useHoverPopover({
    openDelayMs: 0,
    closeDelayMs: 200,
    onOpen: () => {},
    onClose: closeLevel2Now,
  })

  const openLevel1 = useCallback((anchor: HTMLElement, content: ReactNode) => {
    anchor1Ref.current = anchor
    setLevel1({ content })
    setLevel2(null)
  }, [])

  const openLevel2 = useCallback((anchor: HTMLElement, content: ReactNode) => {
    anchor2Ref.current = anchor
    setLevel2({ content })
  }, [])

  const closeAll = useCallback(() => {
    setLevel1(null)
    setLevel2(null)
  }, [])

  // Scroll anywhere on the page invalidates the virtual anchors' positions
  // immediately (plan Step 7: "scroll ปิดทันที") — EXCEPT scrolling inside
  // the popover content itself (its own domain/IP list can be taller than
  // the popover), which must not self-close.
  useEffect(() => {
    if (!level1 && !level2) return
    const handleScroll = (e: Event) => {
      const target = e.target
      if (target instanceof Element && target.closest('[data-slot="popover-content"]')) {
        return
      }
      closeAll()
    }
    window.addEventListener("scroll", handleScroll, true)
    return () => window.removeEventListener("scroll", handleScroll, true)
  }, [level1, level2, closeAll])

  const value: ReferenceHoverContextValue = {
    openLevel1,
    scheduleCloseLevel1: level1Hover.scheduleClose,
    cancelCloseLevel1: level1Hover.cancelClose,
    openLevel2,
    scheduleCloseLevel2: level2Hover.scheduleClose,
    cancelCloseLevel2: level2Hover.cancelClose,
    closeAll,
  }

  return (
    <ReferenceHoverContext.Provider value={value}>
      {children}

      <Popover
        open={level1 !== null && !closeWhen}
        onOpenChange={(open) => {
          if (!open) closeLevel1Now()
        }}
      >
        <PopoverAnchor virtualRef={anchor1Ref} />
        {level1 && (
          <PopoverContent
            className="w-80"
            onOpenAutoFocus={(e) => e.preventDefault()}
            onMouseEnter={level1Hover.cancelClose}
            onMouseLeave={level1Hover.scheduleClose}
          >
            {level1.content}
          </PopoverContent>
        )}
      </Popover>

      <Popover
        open={level2 !== null && !closeWhen}
        onOpenChange={(open) => {
          if (!open) closeLevel2Now()
        }}
      >
        <PopoverAnchor virtualRef={anchor2Ref} />
        {level2 && (
          <PopoverContent
            className="w-80"
            onOpenAutoFocus={(e) => e.preventDefault()}
            onMouseEnter={level2Hover.cancelClose}
            onMouseLeave={level2Hover.scheduleClose}
          >
            {level2.content}
          </PopoverContent>
        )}
      </Popover>
    </ReferenceHoverContext.Provider>
  )
}
