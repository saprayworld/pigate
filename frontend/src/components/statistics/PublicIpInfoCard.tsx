import { useEffect, useRef, useState } from "react"
import { Globe } from "lucide-react"
import { getErrorMessage } from "@/lib/errors"
import { ipinfoService, IpInfoDisabledError } from "@/services/ipinfoService"
import { PublicIpInfoRows, type PublicIpInfoState } from "./PublicIpInfoRows"

// PublicIpInfoCard — Statistics -> Traffic -> Host page's "Public IP Info"
// card (docs/ref/todo/statistics-host-ipinfo-plan.md T-09), replacing "Top
// peers" when the drilled-in IP is public (T-10 decides WHEN to render this;
// this component only renders its own 4 states once mounted). Sized to slot
// into the same `xl:col-span-1` cell Top peers used to occupy. The actual row
// rendering lives in PublicIpInfoRows.tsx (docs/ref/todo/
// reference-popover-plan.md Step 9), shared with the reference popover's
// IpReferenceContent — this component only owns the fetch/loading state and
// the outer card chrome.

type CardState = PublicIpInfoState

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

      <PublicIpInfoRows state={state} />
    </div>
  )
}
