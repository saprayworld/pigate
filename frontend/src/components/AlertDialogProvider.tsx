import { createContext, useContext, useState, type ReactNode } from "react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"

interface AlertConfig {
  title: string
  message: string
  type: "alert" | "confirm"
  resolve: (value: boolean) => void
}

interface AlertDialogContextType {
  alert: (title: string, message: string) => Promise<void>
  confirm: (title: string, message: string) => Promise<boolean>
}

const AlertDialogContext = createContext<AlertDialogContextType | null>(null)

export function useAlert() {
  const context = useContext(AlertDialogContext)
  if (!context) {
    throw new Error("useAlert must be used within an AlertDialogProvider")
  }
  return context
}

export function AlertDialogProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<AlertConfig | null>(null)

  const alert = (title: string, message: string): Promise<void> => {
    return new Promise<void>((resolve) => {
      setConfig({
        title,
        message,
        type: "alert",
        resolve: () => {
          setConfig(null)
          resolve()
        },
      })
    })
  }

  const confirm = (title: string, message: string): Promise<boolean> => {
    return new Promise((resolve) => {
      setConfig({
        title,
        message,
        type: "confirm",
        resolve: (value: boolean) => {
          setConfig(null)
          resolve(value)
        },
      })
    })
  }

  return (
    <AlertDialogContext.Provider value={{ alert, confirm }}>
      {children}
      <Dialog
        open={config !== null}
        modal={true}
        onOpenChange={(open) => {
          if (!open && config) {
            config.resolve(false)
          }
        }}
      >
        <DialogContent
          showCloseButton={false}
          className="max-w-md w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up"
        >
          <DialogHeader>
            <DialogTitle className="text-lg font-bold text-foreground">
              {config?.title}
            </DialogTitle>
          </DialogHeader>
          <DialogDescription className="text-sm text-muted-foreground leading-relaxed whitespace-pre-line">
            {config?.message}
          </DialogDescription>
          <DialogFooter className="flex flex-row justify-end gap-2 mt-2">
            {config?.type === "confirm" && (
              <Button
                variant="outline"
                onClick={() => config?.resolve(false)}
                className="cursor-pointer"
              >
                ยกเลิก
              </Button>
            )}
            <Button
              className="bg-primary hover:bg-primary/90 text-primary-foreground font-bold cursor-pointer"
              onClick={() => config?.resolve(true)}
            >
              ตกลง
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </AlertDialogContext.Provider>
  )
}
