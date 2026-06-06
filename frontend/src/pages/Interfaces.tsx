export default function Interfaces() {
  return (
    <div className="space-y-6">
      {/* Welcome Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-foreground">Network Interfaces</h1>
        <p className="text-muted-foreground mt-1">Manage physical and virtual network adapters on your Raspberry Pi.</p>
      </div>

      <div className="rounded-xl border border-border bg-card/30 p-6">
        <div className="flex h-60 items-center justify-center rounded-lg border border-dashed border-border bg-muted/40 text-muted-foreground">
          [ Physical & Virtual Interfaces configuration page ]
        </div>
      </div>
    </div>
  )
}
