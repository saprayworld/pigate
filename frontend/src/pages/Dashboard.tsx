import { Activity, Shield, Users, Radio } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"

export default function Dashboard() {
  return (
    <div className="space-y-6">
      {/* Welcome Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-foreground">Dashboard Overview</h1>
        <p className="text-muted-foreground mt-1">Real-time status of your PiGate Firewall & Gateway.</p>
      </div>

      {/* Grid Stats */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {/* Card 1 */}
        <Card size="sm" className="bg-card/50 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-sm font-medium text-muted-foreground">Firewall Status</CardTitle>
            <Shield className="h-4 w-4 text-emerald-500" />
          </CardHeader>
          <CardContent className="space-y-1">
            <div className="flex items-center gap-2">
              <span className="text-2xl font-semibold text-foreground">Active</span>
              <span className="flex h-2 w-2 rounded-full bg-emerald-500 animate-pulse"></span>
            </div>
            <CardDescription className="text-xs text-muted-foreground">nftables kernel module active</CardDescription>
          </CardContent>
        </Card>

        {/* Card 2 */}
        <Card size="sm" className="bg-card/50 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-sm font-medium text-muted-foreground">Total Traffic</CardTitle>
            <Activity className="h-4 w-4 text-cyan-500" />
          </CardHeader>
          <CardContent className="space-y-1">
            <span className="text-2xl font-semibold text-foreground">12.4 GB</span>
            <CardDescription className="text-xs text-muted-foreground">Combined input/output today</CardDescription>
          </CardContent>
        </Card>

        {/* Card 3 */}
        <Card size="sm" className="bg-card/50 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-sm font-medium text-muted-foreground">DHCP Leases</CardTitle>
            <Users className="h-4 w-4 text-indigo-550 dark:text-indigo-400" />
          </CardHeader>
          <CardContent className="space-y-1">
            <span className="text-2xl font-semibold text-foreground">18 Clients</span>
            <CardDescription className="text-xs text-muted-foreground">Active IP address allocations</CardDescription>
          </CardContent>
        </Card>

        {/* Card 4 */}
        <Card size="sm" className="bg-card/50 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-sm font-medium text-muted-foreground">Wi-Fi Status</CardTitle>
            <Radio className="h-4 w-4 text-amber-600 dark:text-amber-500" />
          </CardHeader>
          <CardContent className="space-y-1">
            <span className="text-2xl font-semibold text-foreground">wlan0 Master</span>
            <CardDescription className="text-xs text-muted-foreground">Broadcast SSID: PiGate-Secure</CardDescription>
          </CardContent>
        </Card>
      </div>

      {/* Main Content Area Grid */}
      <div className="grid gap-6 md:grid-cols-3">
        {/* Network Graph Placeholder */}
        <Card className="md:col-span-2 bg-card/30">
          <CardHeader>
            <CardTitle className="text-lg text-foreground">Network Bandwidth (Real-time)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex h-60 items-center justify-center rounded-lg border border-dashed border-border bg-muted/40 text-muted-foreground">
              [ Recharts graph will be mounted here in the next step ]
            </div>
          </CardContent>
        </Card>

        {/* Quick Log Placeholder */}
        <Card className="bg-card/30">
          <CardHeader>
            <CardTitle className="text-lg text-foreground">Recent Firewall Logs</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {[
              { time: "11:24:02", action: "BLOCK", src: "192.168.1.104", dest: "10.0.0.1", port: "22" },
              { time: "11:23:45", action: "ALLOW", src: "192.168.1.100", dest: "8.8.8.8", port: "443" },
              { time: "11:21:10", action: "BLOCK", src: "185.220.101.4", dest: "WAN IP", port: "445" },
            ].map((log, idx) => (
              <div key={idx} className="flex flex-col space-y-1 rounded-lg border border-border bg-muted/30 p-3 text-xs">
                <div className="flex justify-between items-center">
                  <span className="text-muted-foreground font-mono">{log.time}</span>
                  <Badge variant={log.action === "BLOCK" ? "destructive" : "outline"} className={`h-4.5 rounded px-1.5 text-[10px] font-bold ${
                    log.action === "ALLOW" ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-450 border-emerald-500/20 hover:bg-emerald-500/20" : ""
                  }`}>
                    {log.action}
                  </Badge>
                </div>
                <div className="text-foreground/90 font-mono mt-1">
                  {log.src} ➔ {log.dest}:{log.port}
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
