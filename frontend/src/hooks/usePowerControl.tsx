import { useCallback, useState } from "react"

import { PowerStatusOverlay, type PowerStatus } from "@/components/power-control"
import { systemService } from "@/services/systemService"
import { useAlert } from "@/hooks/useAlert"
import { getErrorMessage } from "@/lib/errors"

// Owns power-action state and drives the backend reboot/shutdown endpoints so
// the behaviour is shared between the Settings page and the sidebar user menu.
// Confirmation is left to the caller — each surface confirms in its own style
// (a styled Dialog on Settings, useAlert().confirm in the user menu) — and the
// returned `overlay` renders the shared full-screen status screens.
export function usePowerControl() {
  const { alert } = useAlert()
  const [status, setStatus] = useState<PowerStatus>("idle")
  const [countdown, setCountdown] = useState(5)

  // Tick down the reboot countdown and flip back to idle when it reaches zero.
  const startCountdown = useCallback((from: number) => {
    let count = from
    setCountdown(count)
    const interval = setInterval(() => {
      count -= 1
      setCountdown(count)
      if (count <= 0) {
        clearInterval(interval)
        setStatus("idle")
      }
    }, 1000)
  }, [])

  const reboot = useCallback(async () => {
    setStatus("rebooting")
    try {
      await systemService.reboot()
    } catch (err) {
      await alert("ข้อผิดพลาด", "Failed to reboot system: " + getErrorMessage(err))
      setStatus("idle")
      return
    }
    startCountdown(5)
  }, [alert, startCountdown])

  const shutdown = useCallback(async () => {
    setStatus("shutting-down")
    try {
      await systemService.shutdown()
      setTimeout(() => {
        setStatus("powered-off")
      }, 3000)
    } catch (err) {
      await alert("ข้อผิดพลาด", "Failed to shutdown system: " + getErrorMessage(err))
      setStatus("idle")
    }
  }, [alert])

  // Simulated power-on from the powered-off overlay (no real endpoint yet).
  const powerOn = useCallback(() => {
    setStatus("rebooting")
    startCountdown(3)
  }, [startCountdown])

  const overlay = (
    <PowerStatusOverlay status={status} countdown={countdown} onPowerOn={powerOn} />
  )

  return { status, countdown, reboot, shutdown, powerOn, overlay }
}
