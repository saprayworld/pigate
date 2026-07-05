import { useContext } from "react"
import { AlertDialogContext } from "@/hooks/alert-context"

export function useAlert() {
  const context = useContext(AlertDialogContext)
  if (!context) {
    throw new Error("useAlert must be used within an AlertDialogProvider")
  }
  return context
}
