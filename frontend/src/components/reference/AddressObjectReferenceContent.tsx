import { useNavigate } from "react-router"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { classifyReferenceTarget } from "@/lib/referenceTarget"
import { ReferenceTrigger } from "./ReferenceTrigger"
import { IpReferenceContent } from "./IpReferenceContent"
import { DomainReferenceContent } from "./DomainReferenceContent"

// AddressEntry/AddressObject are structural subsets of
// data-mockup/mockData.ts's types (kept local/minimal so this file never
// needs to import the whole mock-data module just for two small shapes —
// PolicyChainPage.tsx passes its already-loaded objects straight in).
export interface AddressObjectReferenceEntry {
  type: "subnet" | "range" | "fqdn"
  value: string
}
export interface AddressObjectReferenceObject {
  name: string
  system: boolean
  // Legacy compat mirror, same convention as AddressObject.type/value —
  // used ONLY as the entries fallback below.
  type: "subnet" | "range" | "fqdn"
  value: string
  entries?: AddressObjectReferenceEntry[]
}

const MAX_VISIBLE_ENTRIES = 5

function entryTypeLabel(type: AddressObjectReferenceEntry["type"]): string {
  switch (type) {
    case "subnet":
      return "Subnet"
    case "range":
      return "IP Range"
    case "fqdn":
      return "FQDN"
  }
}

// AddressObjectReferenceContent — the reference popover's level-1 content for
// a Policy page badge that names an Address Object (docs/ref/todo/
// reference-popover-plan.md Step 11): shows every entry (falling back to the
// legacy top-level type/value pair when `entries` is undefined — plan §5
// Caution 5, old localStorage payloads never crash this). Entries that are a
// single IP or an FQDN get a level-2 ReferenceTrigger; a wide subnet/range
// entry is plain, non-interactive text — level 1 itself NEVER makes a
// network request (plan Step 11: "popover ระดับ 1 ต้องไม่มี network request
// เลย" — only entering a level-2 entry does, via IpReferenceContent/
// DomainReferenceContent's own effects).
export function AddressObjectReferenceContent({ object }: { object: AddressObjectReferenceObject }) {
  const navigate = useNavigate()
  const entries: AddressObjectReferenceEntry[] =
    object.entries && object.entries.length > 0 ? object.entries : [{ type: object.type, value: object.value }]

  const visible = entries.slice(0, MAX_VISIBLE_ENTRIES)
  const hiddenCount = entries.length - visible.length

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <p className="truncate text-sm font-medium text-foreground">{object.name}</p>
        {object.system && (
          <Badge variant="outline" className="shrink-0 border-muted-foreground/30 text-[10px] font-normal text-muted-foreground">
            System
          </Badge>
        )}
      </div>

      <ul className="space-y-1">
        {visible.map((entry, i) => {
          const target = classifyReferenceTarget(entry.value)
          const interactive = target.kind === "ip" || target.kind === "domain"

          const row = (
            <div className="flex min-w-0 items-center justify-between gap-2 text-xs">
              <span className="truncate font-mono text-foreground/90">{entry.value}</span>
              <Badge variant="outline" className="shrink-0 border-muted-foreground/30 text-[10px] font-normal text-muted-foreground">
                {entryTypeLabel(entry.type)}
              </Badge>
            </div>
          )

          return (
            <li key={`${entry.type}-${entry.value}-${i}`}>
              {interactive ? (
                <ReferenceTrigger
                  level={2}
                  content={() =>
                    target.kind === "ip" ? (
                      <IpReferenceContent key={target.value} ip={target.value} />
                    ) : (
                      <DomainReferenceContent key={target.value} domain={target.value} />
                    )
                  }
                >
                  {row}
                </ReferenceTrigger>
              ) : (
                row
              )}
            </li>
          )
        })}
      </ul>

      {hiddenCount > 0 && (
        <p className="text-xs text-muted-foreground">และอีก {hiddenCount} รายการ</p>
      )}

      <div className="pt-1">
        <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => navigate("/policy/addresses")}>
          ดู Address Objects
        </Button>
      </div>
    </div>
  )
}
