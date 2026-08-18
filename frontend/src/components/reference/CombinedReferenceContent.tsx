import { Separator } from "@/components/ui/separator"
import { IpReferenceContent } from "./IpReferenceContent"
import { DomainReferenceContent } from "./DomainReferenceContent"

// CombinedReferenceContent — a single popover covering BOTH an IP and a
// domain that appear together in the same table cell (docs/ref/todo/
// reference-popover-plan.md §0 item 3 — TrafficLogPage.tsx's IpCell has
// `ip`/`domain`/`hostname` all in one cell). Renders both sub-contents
// stacked with a divider, rather than opening two separate popovers.
export function CombinedReferenceContent({ ip, domain }: { ip: string; domain: string }) {
  return (
    <div className="space-y-3">
      <IpReferenceContent key={ip} ip={ip} />
      <Separator />
      <DomainReferenceContent key={domain} domain={domain} />
    </div>
  )
}
