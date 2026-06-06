export default function DhcpServer() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-white">DHCP Server</h1>
        <p className="text-muted-foreground mt-1">Configure IP address allocation pools and static leases.</p>
      </div>

      <div className="rounded-xl border border-neutral-800 bg-neutral-900/30 p-6">
        <div className="flex h-60 items-center justify-center rounded-lg border border-dashed border-neutral-800 bg-neutral-950/40 text-neutral-500">
          [ DHCP Server configuration page ]
        </div>
      </div>
    </div>
  )
}
