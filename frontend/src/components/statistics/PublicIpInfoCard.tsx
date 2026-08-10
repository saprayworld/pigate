import { useEffect, useRef, useState } from "react"
import { Globe } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import { getErrorMessage } from "@/lib/errors"
import { ipinfoService, IpInfoDisabledError, type IpInfoLookup } from "@/services/ipinfoService"

// PublicIpInfoCard — Statistics -> Traffic -> Host page's "Public IP Info"
// card (docs/ref/todo/statistics-host-ipinfo-plan.md T-09), replacing "Top
// peers" when the drilled-in IP is public (T-10 decides WHEN to render this;
// this component only renders its own 4 states once mounted). Sized to slot
// into the same `xl:col-span-1` cell Top peers used to occupy.

type CardState =
  | { kind: "loading" }
  | { kind: "disabled" }
  | { kind: "error"; message: string }
  | { kind: "success"; data: IpInfoLookup }

// Row is a single label/value line. Fields the backend didn't send are
// simply never rendered (no row at all) — plan T-09: "ห้ามแสดง —" for a
// missing field, unlike most other tables in this app.
function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-3 text-xs">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate text-right font-mono text-foreground">{value}</span>
    </div>
  )
}

// NOTE for the caller: this component resets to the "loading" state ONLY on
// mount, not on every `ip` prop change (avoiding a synchronous setState
// inside the effect body) — the caller must pass `key={ip}` so React remounts
// a fresh instance whenever the drilled-in IP changes (StatisticsTrafficHost.tsx
// already does this, since ConversationTable itself unmounts/remounts per tab).
export function PublicIpInfoCard({ ip }: { ip: string }) {
  const [state, setState] = useState<CardState>({ kind: "loading" })
  const requestIdRef = useRef(0)

  useEffect(() => {
    const requestId = ++requestIdRef.current

    ipinfoService
      .getIpInfo(ip)
      .then((data) => {
        if (requestIdRef.current !== requestId) return
        setState({ kind: "success", data })
      })
      .catch((err) => {
        if (requestIdRef.current !== requestId) return
        if (err instanceof IpInfoDisabledError) {
          setState({ kind: "disabled" })
          return
        }
        setState({ kind: "error", message: getErrorMessage(err) })
      })
  }, [ip])

  return (
    <div className="space-y-2 rounded-lg border bg-muted/20 p-3 xl:col-span-1">
      <p className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Globe className="size-3.5" />
        Public IP Info
      </p>

      {state.kind === "loading" && (
        <div className="space-y-2 pt-1">
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-3/4" />
          <Skeleton className="h-3 w-2/3" />
          <Skeleton className="h-3 w-1/2" />
        </div>
      )}

      {state.kind === "disabled" && (
        <p className="pt-1 text-xs text-muted-foreground">
          ฟีเจอร์นี้ปิดอยู่ — เปิดได้ที่ไฟล์คอนฟิก{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">/var/lib/pigate/pigate.conf</code>{" "}
          ด้วยคีย์ <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">ipinfo-enabled</code>
        </p>
      )}

      {state.kind === "error" && <p className="pt-1 text-xs text-muted-foreground">{state.message}</p>}

      {state.kind === "success" && (
        <div className="space-y-1.5 pt-1">
          <Row label="IP" value={state.data.ip} />
          {state.data.hostname && <Row label="Hostname" value={state.data.hostname} />}
          {(state.data.city || state.data.region) && (
            <Row label="City" value={[state.data.city, state.data.region].filter(Boolean).join(", ")} />
          )}
          {(state.data.country || state.data.countryName) && (
            <Row label="Country" value={state.data.countryName || state.data.country || ""} />
          )}
          {state.data.org && <Row label="Org" value={state.data.org} />}
          {state.data.asn && <Row label="ASN" value={state.data.asn} />}
        </div>
      )}
    </div>
  )
}
