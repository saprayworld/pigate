import { useNavigate } from "react-router"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

// ServiceEntry/ServiceObject are structural subsets of
// data-mockup/mockData.ts's types (kept local/minimal so this file never
// needs to import the whole mock-data module just for two small shapes —
// PolicyChainPage.tsx/RuleStatsDrawer.tsx pass their already-loaded objects
// straight in).
export interface ServiceObjectReferenceEntry {
  protocol: "TCP" | "UDP" | "TCP/UDP" | "ICMP"
  port: string
}
export interface ServiceObjectReferenceObject {
  name: string
  type: "system" | "custom"
  // Legacy compat mirror, same convention as ServiceObject.protocol/port —
  // used ONLY as the entries fallback below.
  protocol: "TCP" | "UDP" | "TCP/UDP" | "ICMP"
  port: string
  entries?: ServiceObjectReferenceEntry[]
}

const MAX_VISIBLE_ENTRIES = 5

function portLabel(entry: ServiceObjectReferenceEntry): string {
  if (entry.protocol === "ICMP" && (!entry.port || entry.port === "-")) {
    return "ทุกประเภท"
  }
  return entry.port
}

// ServiceObjectReferenceContent — the reference popover's (only) level of
// content for a Policy page badge that names a Service Object
// (docs/ref/todo/service-object-popover-plan.md Step 1): shows every entry
// (falling back to the legacy top-level protocol/port pair when `entries`
// is undefined — plan §5 Caution 1, old localStorage payloads never crash
// this). A Service Object entry is just a protocol/port pair with no
// reference data of its own, so unlike AddressObjectReferenceContent there
// is no level-2 trigger here and no network request whatsoever (plan §2.1).
export function ServiceObjectReferenceContent({ object }: { object: ServiceObjectReferenceObject }) {
  const navigate = useNavigate()
  const entries: ServiceObjectReferenceEntry[] =
    object.entries && object.entries.length > 0 ? object.entries : [{ protocol: object.protocol, port: object.port }]

  const visible = entries.slice(0, MAX_VISIBLE_ENTRIES)
  const hiddenCount = entries.length - visible.length

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <p className="truncate text-sm font-medium text-foreground">{object.name}</p>
        {object.type === "system" && (
          <Badge variant="outline" className="shrink-0 border-muted-foreground/30 text-[10px] font-normal text-muted-foreground">
            System
          </Badge>
        )}
      </div>

      <ul className="space-y-1">
        {visible.map((entry, i) => (
          <li key={`${entry.protocol}-${entry.port}-${i}`}>
            <div className="flex min-w-0 items-center justify-between gap-2 text-xs">
              <span className="truncate font-mono text-foreground/90">{portLabel(entry)}</span>
              <Badge variant="outline" className="shrink-0 border-muted-foreground/30 text-[10px] font-normal text-muted-foreground">
                {entry.protocol}
              </Badge>
            </div>
          </li>
        ))}
      </ul>

      {hiddenCount > 0 && (
        <p className="text-xs text-muted-foreground">และอีก {hiddenCount} รายการ</p>
      )}

      <div className="pt-1">
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          onClick={() => navigate(`/policy/services?q=${encodeURIComponent(object.name)}`)}
        >
          ดู Service Objects
        </Button>
      </div>
    </div>
  )
}
