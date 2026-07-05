import { useState } from "react"
import { getErrorMessage } from "@/lib/errors"
import type { FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { Lock, AlertCircle, Loader2, Key, CheckCircle2 } from "lucide-react"
import { systemService } from "@/services/systemService"
import { authService } from "@/services/authService"

export default function ForceChangePassword() {
  const [currentPassword, setCurrentPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [confirmPassword, setConfirmPassword] = useState("")
  const [error, setError] = useState("")
  const [success, setSuccess] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const navigate = useNavigate()

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError("")

    if (newPassword.length < 8) {
      setError("New password must be at least 8 characters long.")
      return
    }

    if (newPassword !== confirmPassword) {
      setError("New passwords do not match.")
      return
    }

    setIsSubmitting(true)
    try {
      await systemService.changePassword(currentPassword, newPassword)
      setSuccess(true)
      localStorage.removeItem("pigate_must_change_password")
      setTimeout(() => {
        navigate("/")
      }, 1500)
    } catch (err) {
      setError(getErrorMessage(err) || "Failed to change password. Please verify current password.")
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleLogout = async () => {
    try {
      await authService.logout()
      navigate("/login")
    } catch {
      navigate("/login")
    }
  }

  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-background px-4 py-12 sm:px-6 lg:px-8">
      <div className="w-full max-w-md space-y-8 rounded-2xl border border-border bg-card p-8">
        <div className="flex flex-col items-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10 border border-primary/20 text-primary animate-pulse">
            <Key className="h-6 w-6" />
          </div>
          <h2 className="mt-6 text-center text-3xl font-extrabold tracking-tight text-foreground">
            Change Password
          </h2>
          <p className="mt-2 text-center text-sm text-muted-foreground">
            For security reasons, you must change your initial auto-generated password before proceeding.
          </p>
        </div>

        {success ? (
          <div className="rounded-lg border border-primary/20 bg-primary/10 p-4 text-center text-sm text-primary flex flex-col items-center gap-2">
            <CheckCircle2 className="h-8 w-8 text-primary animate-bounce" />
            <span className="font-medium text-base">Password Changed Successfully!</span>
            <span className="text-muted-foreground text-xs">Redirecting to dashboard...</span>
          </div>
        ) : (
          <form className="mt-8 space-y-6" onSubmit={handleSubmit}>
            {error && (
              <div className="flex items-center gap-2 rounded-lg border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
                <AlertCircle className="h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <div className="space-y-4 rounded-md">
              <div className="relative">
                <label htmlFor="currentPassword" className="sr-only">Current Password</label>
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-muted-foreground">
                  <Lock className="h-4 w-4" />
                </div>
                <input
                  id="currentPassword"
                  name="currentPassword"
                  type="password"
                  required
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  className="block w-full rounded-lg border border-border bg-muted/30 py-3 pl-10 pr-3 text-sm text-foreground placeholder-muted-foreground focus:border-primary focus:ring-1 focus:ring-primary outline-none transition"
                  placeholder="Current Password"
                />
              </div>

              <div className="relative">
                <label htmlFor="newPassword" className="sr-only">New Password</label>
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-muted-foreground">
                  <Lock className="h-4 w-4" />
                </div>
                <input
                  id="newPassword"
                  name="newPassword"
                  type="password"
                  required
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  className="block w-full rounded-lg border border-border bg-muted/30 py-3 pl-10 pr-3 text-sm text-foreground placeholder-muted-foreground focus:border-primary focus:ring-1 focus:ring-primary outline-none transition"
                  placeholder="New Password (min 8 chars)"
                />
              </div>

              <div className="relative">
                <label htmlFor="confirmPassword" className="sr-only">Confirm New Password</label>
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-muted-foreground">
                  <Lock className="h-4 w-4" />
                </div>
                <input
                  id="confirmPassword"
                  name="confirmPassword"
                  type="password"
                  required
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  className="block w-full rounded-lg border border-border bg-muted/30 py-3 pl-10 pr-3 text-sm text-foreground placeholder-muted-foreground focus:border-primary focus:ring-1 focus:ring-primary outline-none transition"
                  placeholder="Confirm New Password"
                />
              </div>
            </div>

            <div className="flex flex-col gap-3">
              <button
                type="submit"
                disabled={isSubmitting}
                className="group relative flex w-full justify-center items-center rounded-lg border border-primary/20 bg-primary px-4 py-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-background transition cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin mr-2" />
                    Updating Password...
                  </>
                ) : (
                  "Change Password"
                )}
              </button>

              <button
                type="button"
                onClick={handleLogout}
                className="group relative flex w-full justify-center items-center rounded-lg border border-border bg-muted/20 hover:bg-muted/50 px-4 py-2 text-sm font-medium text-muted-foreground focus:outline-none transition cursor-pointer"
              >
                Cancel and Sign Out
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}
