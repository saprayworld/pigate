import { useCallback, useRef, useState } from "react"

import { PowerStatusOverlay, type PowerStatus } from "@/components/power-control"
import { systemService } from "@/services/systemService"
import { IS_MOCK_MODE, API_BASE_URL } from "@/services/config"
import { useAlert } from "@/hooks/useAlert"
import { getErrorMessage } from "@/lib/errors"

// A Raspberry Pi 5 takes roughly 20-40s to come back after a reboot, so the
// countdown starts here purely as a reassuring estimate — the actual "back
// online" signal is the backend answering the poll below, not the timer.
const REBOOT_ESTIMATE_SECONDS = 30

// How often (ms) we poll the backend after a reboot to detect it is up again,
// and how long we keep trying before giving up and reloading anyway.
const POLL_INTERVAL_MS = 2500
const POLL_TIMEOUT_MS = 5 * 60 * 1000

// Owns power-action state and drives the backend reboot/shutdown endpoints so
// the behaviour is shared between the Settings page and the sidebar user menu.
// Confirmation is left to the caller — each surface confirms in its own style
// (a styled Dialog on Settings, useAlert().confirm in the user menu) — and the
// returned `overlay` renders the shared full-screen status screens.
export function usePowerControl() {
  const { alert } = useAlert()
  const [status, setStatus] = useState<PowerStatus>("idle")
  const [countdown, setCountdown] = useState(REBOOT_ESTIMATE_SECONDS)
  const pollTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Tick down the reboot countdown. onDone fires once it reaches zero; the
  // overlay renders "OK" for countdown <= 0, so leaving it there (real reboot,
  // while we keep polling) is fine.
  const startCountdown = useCallback((from: number, onDone?: () => void) => {
    let count = from
    setCountdown(count)
    const interval = setInterval(() => {
      count -= 1
      setCountdown(count)
      if (count <= 0) {
        clearInterval(interval)
        onDone?.()
      }
    }, 1000)
  }, [])

  // Poll a lightweight endpoint until the backend answers again (any HTTP
  // response, even 401, proves the server is back), then hard-reload so the app
  // re-authenticates cleanly. A network error means it is still down → keep
  // trying until POLL_TIMEOUT_MS, after which we reload regardless.
  const waitForBackendThenReload = useCallback(() => {
    const deadline = Date.now() + POLL_TIMEOUT_MS
    const poll = async () => {
      try {
        await fetch(`${API_BASE_URL}/auth/session`, { method: "GET", cache: "no-store" })
        window.location.reload()
        return
      } catch {
        // Still unreachable (board rebooting); fall through to retry.
      }
      if (Date.now() >= deadline) {
        window.location.reload()
        return
      }
      pollTimer.current = setTimeout(poll, POLL_INTERVAL_MS)
    }
    // Wait one interval before the first probe — the board hasn't even started
    // shutting down services yet.
    pollTimer.current = setTimeout(poll, POLL_INTERVAL_MS)
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
    if (IS_MOCK_MODE) {
      // Dev/mock: nothing actually reboots, so just run the countdown and return
      // to idle instead of polling for a reconnect that never drops.
      startCountdown(5, () => setStatus("idle"))
      return
    }
    // Real board: it is going down now. Show the estimate countdown and poll in
    // parallel; whichever the board's actual reboot time is, the poll drives the
    // reload once it answers again.
    startCountdown(REBOOT_ESTIMATE_SECONDS)
    waitForBackendThenReload()
  }, [alert, startCountdown, waitForBackendThenReload])

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
    startCountdown(3, () => setStatus("idle"))
  }, [startCountdown])

  const overlay = (
    <PowerStatusOverlay status={status} countdown={countdown} onPowerOn={powerOn} />
  )

  return { status, countdown, reboot, shutdown, powerOn, overlay }
}
