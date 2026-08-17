import { useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes } from "@/lib/formatBytes"
import { UpDownLine } from "@/components/statistics/HostCells"
import { referenceService, type DomainReferenceSummary } from "@/services/referenceService"
import { DnsLoggingDisabledNotice } from "./DnsLoggingDisabledNotice"

// DomainReferenceContent — the reference popover's Domain mode (docs/ref/todo/
// reference-popover-plan.md Step 9): resolved IPs (≤ limit) + "+N more" ->
// navigate to the domain drill-down page, exactly like IpReferenceContent's
// domain list.
//
// NOTE for callers: same "resets only on mount" convention as
// IpReferenceContent — pass `key={domain}` at every call site.
export function DomainReferenceContent({ domain }: { domain: string }) {
  const [summary, setSummary] = useState<DomainReferenceSummary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  useEffect(() => {
    let cancelled = false
    referenceService
      .getDomainReference(domain)
      .then((s) => {
        if (!cancelled) setSummary(s)
      })
      .catch((err) => {
        if (!cancelled) setError(getErrorMessage(err))
      })
    return () => {
      cancelled = true
    }
  }, [domain])

  if (error) {
    return <p className="text-xs text-muted-foreground">{error}</p>
  }
  if (!summary) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-4 w-2/3" />
        <Skeleton className="h-3 w-1/2" />
        <Skeleton className="h-10 w-full" />
      </div>
    )
  }

  const hiddenIpCount = summary.ipCount - summary.ips.length

  return (
    <div className="space-y-3">
      <p className="truncate text-sm font-medium text-foreground">{summary.domain}</p>

      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="text-muted-foreground">Traffic ({summary.window})</span>
        {summary.bytes > 0 ? <UpDownLine up={summary.bytesUp} down={summary.bytesDown} /> : <span className="font-mono text-muted-foreground">{fmtBytes(0)}</span>}
      </div>

      {!summary.enabled ? (
        <DnsLoggingDisabledNotice />
      ) : (
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <p className="text-[11px] font-medium text-muted-foreground">IP ที่ resolve ไป</p>
            {summary.sharedIPs && (
              <Badge variant="outline" className="border-primary/30 text-[10px] font-normal text-primary">
                Shared
              </Badge>
            )}
          </div>
          {summary.ips.length === 0 ? (
            <p className="text-xs text-muted-foreground">ไม่พบข้อมูล IP ที่เกี่ยวข้อง</p>
          ) : (
            <ul className="space-y-0.5">
              {summary.ips.map((ref) => (
                <li key={ref.ip} className="truncate font-mono text-xs text-foreground/90">
                  {ref.ip}
                  {ref.shared && <span className="ml-1 text-[10px] text-muted-foreground">(shared)</span>}
                </li>
              ))}
            </ul>
          )}
          {hiddenIpCount > 0 && (
            <Button
              variant="link"
              size="sm"
              className="h-auto p-0 text-xs"
              onClick={() => navigate(`/statistics/dns/domain/${encodeURIComponent(summary.domain)}`)}
            >
              +{hiddenIpCount} more
            </Button>
          )}
        </div>
      )}

      <div className="flex flex-wrap gap-2 pt-1">
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          onClick={() => navigate(`/statistics/dns/domain/${encodeURIComponent(summary.domain)}`)}
        >
          ดูรายละเอียด
        </Button>
      </div>
    </div>
  )
}
