import { useState } from "react"
import { getErrorMessage } from "@/lib/errors"
import type { FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { Lock, User, AlertCircle, Loader2 } from "lucide-react"
import { authService } from "@/services/authService"
import { PiGateLogo } from "@/components/PiGateLogo"

export default function Login() {
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [isSubmitting, setIsSubmitting] = useState(false)
  const navigate = useNavigate()

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError("")
    setIsSubmitting(true)
    try {
      await authService.login(username, password)
      navigate("/")
    } catch (err) {
      setError(getErrorMessage(err) || "Invalid username or password.")
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-background px-4 py-12 sm:px-6 lg:px-8">
      <div className="w-full max-w-md space-y-8 rounded-2xl border border-border bg-card p-8">
        <div className="flex flex-col items-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl ">
            <PiGateLogo className="size-12 shrink-0" />
          </div>
          <h2 className="mt-6 text-center text-3xl font-extrabold tracking-tight text-foreground">
            PiGate
          </h2>
          <p className="mt-2 text-center text-sm text-muted-foreground">
            Enter credentials to access administrative panel
          </p>
        </div>

        <form className="mt-8 space-y-6" onSubmit={handleSubmit}>
          {error && (
            <div className="flex items-center gap-2 rounded-lg border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          <div className="space-y-4 rounded-md">
            <div className="relative">
              <label htmlFor="username" className="sr-only">Username</label>
              <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-muted-foreground">
                <User className="h-4 w-4" />
              </div>
              <input
                id="username"
                name="username"
                type="text"
                required
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="block w-full rounded-lg border border-border bg-muted/30 py-3 pl-10 pr-3 text-sm text-foreground placeholder-muted-foreground focus:border-primary focus:ring-1 focus:ring-primary outline-none transition"
                placeholder="Username"
              />
            </div>
            <div className="relative">
              <label htmlFor="password" className="sr-only">Password</label>
              <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-muted-foreground">
                <Lock className="h-4 w-4" />
              </div>
              <input
                id="password"
                name="password"
                type="password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="block w-full rounded-lg border border-border bg-muted/30 py-3 pl-10 pr-3 text-sm text-foreground placeholder-muted-foreground focus:border-primary focus:ring-1 focus:ring-primary outline-none transition"
                placeholder="Password"
              />
            </div>
          </div>

          <div>
            <button
              type="submit"
              disabled={isSubmitting}
              className="group relative flex w-full justify-center items-center rounded-lg border border-primary/20 bg-primary px-4 py-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-background transition cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin mr-2" />
                  Signing In...
                </>
              ) : (
                "Sign In"
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
