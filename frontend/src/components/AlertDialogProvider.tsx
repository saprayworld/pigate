import { useState, useCallback, type ReactNode } from "react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { AlertDialogContext } from "@/hooks/alert-context"

interface AlertConfig {
  title: string
  message: string
  type: "alert" | "confirm"
  resolve: (value: boolean) => void
}

export function AlertDialogProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<AlertConfig | null>(null)

  // Stable identity (useCallback + setState setter only) so effects that call
  // alert()/confirm() on an error path can safely list them as a dependency.
  const alert = useCallback((title: string, message: string): Promise<void> => {
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
  }, [])

  const confirm = useCallback((title: string, message: string): Promise<boolean> => {
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
  }, [])

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
