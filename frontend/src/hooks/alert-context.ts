import { createContext } from "react"

export interface AlertDialogContextType {
  alert: (title: string, message: string) => Promise<void>
  confirm: (title: string, message: string) => Promise<boolean>
}

export const AlertDialogContext = createContext<AlertDialogContextType | null>(null)
