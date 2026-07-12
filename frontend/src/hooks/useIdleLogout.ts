import { useEffect, useRef } from "react"

import { authService } from "@/services/authService"

const IDLE_LOGOUT_MS = 5 * 60_000 // 5 minutes of no real user input
const ACTIVITY_KEY = "pigate_last_activity"
const WRITE_THROTTLE_MS = 5_000 // at most one localStorage write per 5s
const CHECK_INTERVAL_MS = 15_000 // how often we compare against the threshold
const ACTIVITY_EVENTS = [
  "mousemove",
  "mousedown",
  "keydown",
  "touchstart",
  "wheel",
] as const

/**
 * useIdleLogout logs the user out after IDLE_LOGOUT_MS of no real mouse/keyboard
 * activity (FortiGate admintimeout style), then bounces to /login. The backend
 * cannot enforce a 5-minute idle window on its own because the dashboard/header
 * poll the API every few seconds on every page — the server can't tell a poll
 * from a human. Only the frontend sees real input events, so it is the component
 * that decides "the human walked away" and calls authService.logout(), which
 * revokes the session server-side (RemoveSession) — a real revocation, not just
 * a UI clear. The server-side idle TTL is the backstop for a closed/crashed tab.
 *
 * The "last activity" timestamp is shared across tabs through localStorage
 * (Caution 1): all tabs read/write one ACTIVITY_KEY, so an idle background tab
 * never terminates the session of a tab the user is actively using — they share
 * a single cookie. Mount this once, under the protected shell layout.
 */
export function useIdleLogout() {
  const lastWriteRef = useRef(0)
  const loggedOutRef = useRef(false)

  useEffect(() => {
    // Seed activity to "now" so a stale or missing key from a previous session
    // doesn't bounce the user the instant they log in (Caution 2).
    const seed = Date.now()
    localStorage.setItem(ACTIVITY_KEY, String(seed))
    lastWriteRef.current = seed

    const recordActivity = () => {
      const now = Date.now()
      // mousemove fires per-pixel; one write every few seconds is ample against a
      // 5-minute threshold (Caution 10).
      if (now - lastWriteRef.current < WRITE_THROTTLE_MS) return
      lastWriteRef.current = now
      localStorage.setItem(ACTIVITY_KEY, String(now))
    }

    ACTIVITY_EVENTS.forEach((evt) =>
      window.addEventListener(evt, recordActivity, { passive: true }),
    )

    const interval = window.setInterval(() => {
      if (loggedOutRef.current) return
      const raw = localStorage.getItem(ACTIVITY_KEY)
      // A missing key counts as "just active" rather than "idle forever", so we
      // never bounce on a cleared key (Caution 2).
      const last = raw ? Number(raw) : Date.now()
      if (Date.now() - last > IDLE_LOGOUT_MS) {
        loggedOutRef.current = true
        void authService.logout().finally(() => {
          window.location.href = "/login"
        })
      }
    }, CHECK_INTERVAL_MS)

    return () => {
      ACTIVITY_EVENTS.forEach((evt) =>
        window.removeEventListener(evt, recordActivity),
      )
      window.clearInterval(interval)
    }
  }, [])
}
