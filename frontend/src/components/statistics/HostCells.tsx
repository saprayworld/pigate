import { ArrowDown, ArrowUp } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { fmtBytes } from "@/lib/formatBytes"
import type { TopHost } from "@/services/statisticsService"

// UpDownLine/HostLabel are shared row-cell components (docs/ref/todo/
// statistics-overview-bandwidth-chart-plan.md T-08A) — moved here verbatim
// (same markup/logic) from what used to be local-only components in
// pages/StatisticsOverview.tsx, so new components (e.g. TopHostsShareCard)
// can reuse them without a circular import (a card importing from the page
// that renders it). Pure move: do not change markup/logic here.

// UpDownLine renders the small "↑ up · ↓ down" byte sub-line shared by
// TopHostsCard rows and the Conversations table (docs/ref/todo/
// statistics-split-upload-download-bytes-plan.md T-11). Uses theme-variable
// colors only (text-primary/text-muted-foreground) — never raw palette
// classes, per rules_of_work.md.
export function UpDownLine({ up, down }: { up: number; down: number }) {
  return (
    <span className="flex items-center gap-2 font-mono text-[10px] text-muted-foreground">
      <span className="flex items-center gap-0.5 text-primary">
        <ArrowDown className="size-2.5" />
        {fmtBytes(down)}
      </span>
      <span className="flex items-center gap-0.5">
        <ArrowUp className="size-2.5" />
        {fmtBytes(up)}
      </span>
    </span>
  )
}

// onClick is OPTIONAL (docs/ref/todo/statistics-traffic-page-plan.md T-12):
// when supplied, the primary label renders as a real <button> (same click
// affordance the Top Queried Domains card already uses — cursor-pointer +
// hover:text-primary hover:underline + a Thai title tooltip) that opens the
// Traffic drill-down for this host; without it, HostLabel renders byte-for-
// byte as before (Dashboard/other callers unaffected).
export function HostLabel({ host, onClick }: { host: TopHost; onClick?: () => void }) {
  // When the destination has a known domain (docs/ref/todo/
  // statistics-dns-top-domain-plan.md T-13), show it as the primary line and
  // demote the IP to a small font-mono label beside it — otherwise the
  // layout is unchanged from before this feature existed (plan §5 item 12).
  if (host.domain) {
    return (
      <span className="flex min-w-0 items-center gap-2">
        <span className="min-w-0">
          {onClick ? (
            <button
              type="button"
              onClick={onClick}
              title="คลิกเพื่อดูรายละเอียดการเชื่อมต่อของเครื่องนี้"
              className="block max-w-full cursor-pointer truncate text-left text-foreground/90 hover:text-primary hover:underline"
            >
              {host.domain}
            </button>
          ) : (
            <span className="block truncate text-foreground/90">{host.domain}</span>
          )}
          <span className="block truncate font-mono text-[10px] text-muted-foreground">{host.ip}</span>
        </span>
        <Badge
          variant="outline"
          className={cn(
            "shrink-0 font-normal text-[10px]",
            host.private ? "border-primary/30 text-primary" : "border-muted-foreground/30 text-muted-foreground"
          )}
        >
          {host.private ? "LAN" : "Internet"}
        </Badge>
      </span>
    )
  }
  return (
    <span className="flex min-w-0 items-center gap-2">
      {onClick ? (
        <button
          type="button"
          onClick={onClick}
          title="คลิกเพื่อดูรายละเอียดการเชื่อมต่อของเครื่องนี้"
          className="max-w-full cursor-pointer truncate text-left text-foreground/90 hover:text-primary hover:underline"
        >
          {host.hostname}
        </button>
      ) : (
        <span className="truncate text-foreground/90">{host.hostname}</span>
      )}
      {host.hostname !== host.ip && (
        <span className="shrink-0 font-mono text-xs text-muted-foreground">{host.ip}</span>
      )}
      <Badge
        variant="outline"
        className={cn(
          "shrink-0 font-normal text-[10px]",
          host.private ? "border-primary/30 text-primary" : "border-muted-foreground/30 text-muted-foreground"
        )}
      >
        {host.private ? "LAN" : "Internet"}
      </Badge>
    </span>
  )
}
