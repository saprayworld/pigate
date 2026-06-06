export default function FirewallPolicy() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-foreground">Firewall Policy</h1>
        <p className="text-muted-foreground mt-1">Configure security policies, rules, and drag-and-drop priorities.</p>
      </div>

      <div className="rounded-xl border border-border bg-card/30 p-6">
        <div className="flex h-60 items-center justify-center rounded-lg border border-dashed border-border bg-muted/40 text-muted-foreground">
          [ Firewall Policies with Drag & Drop ordering page ]
        </div>
      </div>
    </div>
  )
}
