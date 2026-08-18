import { createContext, useContext } from "react"
import type { ReactNode } from "react"

// ReferenceHoverContext wires ReferenceTrigger instances (potentially
// thousands of them, e.g. every row of a 5,000-row Traffic Log table) to the
// SINGLE pair of Popover Roots ReferenceHoverProvider owns (docs/ref/todo/
// reference-popover-plan.md Step 8/§5 Caution 2 — never one Radix Popover
// Root per table row). A trigger never renders its own <Popover>; it only
// calls openLevel(1, anchorEl, content) on hover-intent and lets the
// provider render the popover, anchored (via Radix's virtualRef) at the
// hovered element.
//
// Exactly 2 levels are supported (level 1: a table cell; level 2: an entry
// inside an already-open level-1 popover, e.g. an Address Object's member
// row) — this is a hard ceiling enforced by the type below, not a general
// recursive stack (plan Step 8: "จำกัดในโค้ดชัดเจน ไม่ recursive").
export type ReferenceLevel = 1 | 2

export interface ReferenceHoverContextValue {
  /** Open (or move) the level-1 popover, anchored at `anchor`. Opening level
   * 1 always closes level 2 (a new level-1 target invalidates whatever
   * level-2 detail was drilled into). */
  openLevel1: (anchor: HTMLElement, content: ReactNode) => void
  /** Schedule level 1 (and, transitively, level 2) to close after the
   * hover-bridge grace period. */
  scheduleCloseLevel1: () => void
  /** Cancel a pending level-1 close — call from the level-1 popover
   * content's own onMouseEnter. */
  cancelCloseLevel1: () => void

  /** Open the level-2 popover, anchored at `anchor` — level 1 stays open
   * (plan Step 9's acceptance case: "hover entry IP/FQDN -> popover ระดับ 2
   * เปิดโดยระดับ 1 ไม่ปิด"). */
  openLevel2: (anchor: HTMLElement, content: ReactNode) => void
  scheduleCloseLevel2: () => void
  cancelCloseLevel2: () => void

  /** Close both levels immediately, no grace period — used when a Drawer/
   * Dialog opens elsewhere on the page (plan §5 Caution 3) or on
   * Escape/scroll (plan Step 7). */
  closeAll: () => void
}

export const ReferenceHoverContext = createContext<ReferenceHoverContextValue | null>(null)

// useReferenceHover returns null when called outside a ReferenceHoverProvider
// (rather than throwing) — ReferenceTrigger degrades to a plain, non-
// interactive span in that case, so a page that hasn't been wired up yet
// (Step 13's remaining call sites, or a stray import) never crashes.
export function useReferenceHover(): ReferenceHoverContextValue | null {
  return useContext(ReferenceHoverContext)
}
