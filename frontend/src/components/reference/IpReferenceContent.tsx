import { useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Globe } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes } from "@/lib/formatBytes"
import { UpDownLine } from "@/components/statistics/HostCells"
import { PublicIpInfoRows, type PublicIpInfoState } from "@/components/statistics/PublicIpInfoRows"
import { ipinfoService, IpInfoDisabledError } from "@/services/ipinfoService"
import { referenceService, type IPReferenceSummary } from "@/services/referenceService"
import { DnsLoggingDisabledNotice } from "./DnsLoggingDisabledNotice"

// IpReferenceContent — the reference popover's IP mode (docs/ref/todo/
// reference-popover-plan.md Step 9): 2 shapes decided ENTIRELY by the
// backend's `scope` field (never re-derived client-side — plan §2.3/Caution
// 1, a security boundary). LAN scope never calls /api/statistics/ipinfo.
//
// NOTE for callers (mirrors PublicIpInfoCard.tsx's convention): this
// component resets to the "loading" state ONLY on mount, not on every `ip`
// prop change — pass `key={ip}` at every call site so React remounts a
// fresh instance whenever the hovered IP changes.
export function IpReferenceContent({ ip }: { ip: string }) {
  const [summary, setSummary] = useState<IPReferenceSummary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  useEffect(() => {
    let cancelled = false
    referenceService
      .getIpReference(ip)
      .then((s) => {
        if (!cancelled) setSummary(s)
      })
      .catch((err) => {
        if (!cancelled) setError(getErrorMessage(err))
      })
    return () => {
      cancelled = true
    }
  }, [ip])

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

  const hiddenDomainCount = summary.domainCount - summary.domains.length

  return (
    <div className="space-y-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-foreground">{summary.hostname || summary.ip}</p>
          {summary.hostname && <p className="truncate font-mono text-[10px] text-muted-foreground">{summary.ip}</p>}
          {summary.mac && <p className="truncate font-mono text-[10px] text-muted-foreground">{summary.mac}</p>}
        </div>
        <Badge
          variant="outline"
          className={
            summary.scope === "public"
              ? "shrink-0 border-primary/30 text-[10px] font-normal text-primary"
              : "shrink-0 border-muted-foreground/30 text-[10px] font-normal text-muted-foreground"
          }
        >
          {summary.scope === "public" ? "Internet" : "LAN"}
        </Badge>
      </div>

      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="text-muted-foreground">Traffic ({summary.window})</span>
        {summary.bytes > 0 ? <UpDownLine up={summary.bytesUp} down={summary.bytesDown} /> : <span className="font-mono text-muted-foreground">{fmtBytes(0)}</span>}
      </div>

      {!summary.enabled ? (
        <DnsLoggingDisabledNotice />
      ) : (
        <div className="space-y-1">
          <p className="text-[11px] font-medium text-muted-foreground">
            {summary.scope === "public" ? "Domain ที่ resolve มาเป็น IP นี้" : "Domain ที่เครื่องนี้ query"}
          </p>
          {summary.domains.length === 0 ? (
            <p className="text-xs text-muted-foreground">ไม่พบข้อมูล domain ที่เกี่ยวข้อง</p>
          ) : (
            <ul className="space-y-0.5">
              {summary.domains.map((d) => (
                <li key={d.domain} className="truncate font-mono text-xs text-foreground/90">
                  {d.domain}
                  {typeof d.count === "number" && d.count > 0 && (
                    <span className="ml-1 text-muted-foreground">×{d.count}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
          {hiddenDomainCount > 0 && (
            <Button
              variant="link"
              size="sm"
              className="h-auto p-0 text-xs"
              onClick={() =>
                summary.scope === "public"
                  ? navigate(`/statistics/dns/ip?ip=${encodeURIComponent(summary.ip)}`)
                  : navigate(`/statistics/dns/client/${encodeURIComponent(summary.ip)}`)
              }
            >
              +{hiddenDomainCount} more
            </Button>
          )}
        </div>
      )}

      {summary.scope === "public" && <PublicIpInfoSection ip={summary.ip} />}

      <div className="flex flex-wrap gap-2 pt-1">
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          onClick={() => navigate(`/statistics/traffic/host/${encodeURIComponent(summary.ip)}`)}
        >
          ดู Traffic
        </Button>
        {summary.scope === "lan" && (
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            onClick={() => navigate(`/statistics/dns/client/${encodeURIComponent(summary.ip)}`)}
          >
            ดู DNS
          </Button>
        )}
      </div>
    </div>
  )
}

// PublicIpInfoSection fetches /api/statistics/ipinfo ONLY here — never for a
// "lan" scoped summary above, and only once this whole content component is
// mounted (i.e. the popover already opened for real, plan §5 Caution 4:
// "ยิง ipinfo หลัง popover เปิดจริงเท่านั้น ห้าม prefetch ตอนเริ่ม hover").
// Reuses PublicIpInfoRows.tsx's presentational rows — never a copy of
// PublicIpInfoCard's markup (plan Step 9).
function PublicIpInfoSection({ ip }: { ip: string }) {
  const [state, setState] = useState<PublicIpInfoState>({ kind: "loading" })

  useEffect(() => {
    let cancelled = false
    ipinfoService
      .getIpInfo(ip)
      .then((data) => {
        if (!cancelled) setState({ kind: "success", data })
      })
      .catch((err) => {
        if (cancelled) return
        if (err instanceof IpInfoDisabledError) {
          setState({ kind: "disabled" })
          return
        }
        setState({ kind: "error", message: getErrorMessage(err) })
      })
    return () => {
      cancelled = true
    }
  }, [ip])

  return (
    <div className="space-y-1.5 border-t border-border pt-2">
      <p className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
        <Globe className="size-3" />
        Public IP Info
      </p>
      <PublicIpInfoRows state={state} />
    </div>
  )
}
