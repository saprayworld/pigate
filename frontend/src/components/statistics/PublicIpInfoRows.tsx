import { Skeleton } from "@/components/ui/skeleton"
import type { IpInfoLookup } from "@/services/ipinfoService"

// PublicIpInfoRows — the presentational (fetch-free) inner content shared by
// PublicIpInfoCard.tsx (Statistics -> Traffic -> Host page) and
// IpReferenceContent.tsx (the reference popover, docs/ref/todo/
// reference-popover-plan.md Step 9 — "ห้าม copy ทั้งก้อน" from
// PublicIpInfoCard). Both callers own their own fetch/loading state and pass
// it in here as `state`; this file only renders the 4 states.

export type PublicIpInfoState =
  | { kind: "loading" }
  | { kind: "disabled" }
  | { kind: "error"; message: string }
  | { kind: "success"; data: IpInfoLookup }

// Row is a single label/value line. Fields the backend didn't send are
// simply never rendered (no row at all) — plan T-09: "ห้ามแสดง —" for a
// missing field, unlike most other tables in this app.
export function PublicIpInfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-3 text-xs">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate text-right font-mono text-foreground">{value}</span>
    </div>
  )
}

export function PublicIpInfoRows({ state }: { state: PublicIpInfoState }) {
  return (
    <>
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
          <PublicIpInfoRow label="IP" value={state.data.ip} />
          {state.data.hostname && <PublicIpInfoRow label="Hostname" value={state.data.hostname} />}
          {(state.data.city || state.data.region) && (
            <PublicIpInfoRow label="City" value={[state.data.city, state.data.region].filter(Boolean).join(", ")} />
          )}
          {(state.data.country || state.data.countryName) && (
            <PublicIpInfoRow label="Country" value={state.data.countryName || state.data.country || ""} />
          )}
          {state.data.org && <PublicIpInfoRow label="Org" value={state.data.org} />}
          {state.data.asn && <PublicIpInfoRow label="ASN" value={state.data.asn} />}
        </div>
      )}
    </>
  )
}
