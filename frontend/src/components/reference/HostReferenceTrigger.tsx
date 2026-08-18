import type { ReactNode } from "react"
import { ReferenceTrigger } from "./ReferenceTrigger"
import { IpReferenceContent } from "./IpReferenceContent"
import { DomainReferenceContent } from "./DomainReferenceContent"
import { CombinedReferenceContent } from "./CombinedReferenceContent"
import { classifyReferenceTarget } from "@/lib/referenceTarget"

// HostReferenceTrigger — a thin convenience wrapper around ReferenceTrigger
// for the common "table cell shows a host, wired from separate ip/domain
// fields" case used throughout the Statistics pages, reusing the exact
// ip/domain/combined wiring pattern already proven in
// components/policy/RuleStatsDrawer.tsx (~line 126-144). Callers just pass
// whatever ip/domain string(s) they have on the row — classification
// (valid ip vs valid domain vs neither) and picking Ip/Domain/Combined
// content, plus the required key={ip}/key={domain} remounting, all happen
// here in one place instead of being duplicated per call site.
export function HostReferenceTrigger({
  ip,
  domain,
  className,
  children,
}: {
  ip?: string
  domain?: string
  className?: string
  children: ReactNode
}) {
  const validIp = ip && classifyReferenceTarget(ip).kind === "ip" ? ip : undefined
  const validDomain = domain && classifyReferenceTarget(domain).kind === "domain" ? domain : undefined

  if (!validIp && !validDomain) {
    return <span className={className}>{children}</span>
  }

  return (
    <ReferenceTrigger
      className={className}
      content={() => {
        if (validIp && validDomain) {
          return <CombinedReferenceContent ip={validIp} domain={validDomain} />
        }
        if (validIp) {
          return <IpReferenceContent key={validIp} ip={validIp} />
        }
        return <DomainReferenceContent key={validDomain} domain={validDomain as string} />
      }}
    >
      {children}
    </ReferenceTrigger>
  )
}
