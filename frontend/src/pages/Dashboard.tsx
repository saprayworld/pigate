import { Activity, Shield, Users, Radio } from "lucide-react"

export default function Dashboard() {
  return (
    <div className="space-y-6">
      {/* Welcome Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-white">Dashboard Overview</h1>
        <p className="text-muted-foreground mt-1">Real-time status of your PiGate Firewall & Gateway.</p>
      </div>

      {/* Grid Stats */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {/* Card 1 */}
        <div className="rounded-xl border border-neutral-800 bg-neutral-900/50 p-6 backdrop-blur-sm">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-neutral-400">Firewall Status</span>
            <Shield className="h-4 w-4 text-emerald-500" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-semibold text-white">Active</span>
            <span className="flex h-2 w-2 rounded-full bg-emerald-500 animate-pulse mt-3"></span>
          </div>
          <p className="text-xs text-neutral-500 mt-1">nftables kernel module active</p>
        </div>

        {/* Card 2 */}
        <div className="rounded-xl border border-neutral-800 bg-neutral-900/50 p-6 backdrop-blur-sm">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-neutral-400">Total Traffic</span>
            <Activity className="h-4 w-4 text-cyan-500" />
          </div>
          <div className="mt-2">
            <span className="text-2xl font-semibold text-white">12.4 GB</span>
          </div>
          <p className="text-xs text-neutral-500 mt-1">Combined input/output today</p>
        </div>

        {/* Card 3 */}
        <div className="rounded-xl border border-neutral-800 bg-neutral-900/50 p-6 backdrop-blur-sm">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-neutral-400">DHCP Leases</span>
            <Users className="h-4 w-4 text-indigo-500" />
          </div>
          <div className="mt-2">
            <span className="text-2xl font-semibold text-white">18 Clients</span>
          </div>
          <p className="text-xs text-neutral-500 mt-1">Active IP address allocations</p>
        </div>

        {/* Card 4 */}
        <div className="rounded-xl border border-neutral-800 bg-neutral-900/50 p-6 backdrop-blur-sm">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-neutral-400">Wi-Fi Status</span>
            <Radio className="h-4 w-4 text-amber-500" />
          </div>
          <div className="mt-2">
            <span className="text-2xl font-semibold text-white">wlan0 Master</span>
          </div>
          <p className="text-xs text-neutral-500 mt-1">Broadcast SSID: PiGate-Secure</p>
        </div>
      </div>

      {/* Main Content Area Grid */}
      <div className="grid gap-6 md:grid-cols-3">
        {/* Network Graph Placeholder */}
        <div className="md:col-span-2 rounded-xl border border-neutral-800 bg-neutral-900/30 p-6">
          <h3 className="text-lg font-medium text-white">Network Bandwidth (Real-time)</h3>
          <div className="mt-6 flex h-60 items-center justify-center rounded-lg border border-dashed border-neutral-800 bg-neutral-950/40 text-neutral-500">
            [ Recharts graph will be mounted here in the next step ]
          </div>
        </div>

        {/* Quick Log Placeholder */}
        <div className="rounded-xl border border-neutral-800 bg-neutral-900/30 p-6">
          <h3 className="text-lg font-medium text-white">Recent Firewall Logs</h3>
          <div className="mt-4 space-y-3">
            {[
              { time: "11:24:02", action: "BLOCK", src: "192.168.1.104", dest: "10.0.0.1", port: "22" },
              { time: "11:23:45", action: "ALLOW", src: "192.168.1.100", dest: "8.8.8.8", port: "443" },
              { time: "11:21:10", action: "BLOCK", src: "185.220.101.4", dest: "WAN IP", port: "445" },
            ].map((log, idx) => (
              <div key={idx} className="flex flex-col space-y-1 rounded border border-neutral-800 bg-neutral-950/20 p-2 text-xs">
                <div className="flex justify-between items-center">
                  <span className="text-neutral-400 font-mono">{log.time}</span>
                  <span className={`px-1.5 py-0.5 rounded text-[10px] font-bold ${
                    log.action === "BLOCK" ? "bg-red-950/40 text-red-400 border border-red-900/50" : "bg-emerald-950/40 text-emerald-400 border border-emerald-900/50"
                  }`}>
                    {log.action}
                  </span>
                </div>
                <div className="text-neutral-300 font-mono">
                  {log.src} ➔ {log.dest}:{log.port}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
