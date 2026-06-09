import { useState, useEffect, useMemo } from "react"
import {
  Activity,
  Shield,
  Users,
  Radio,
  Cpu,
  HardDrive,
  Thermometer,
  Clock,
  Search,
  Trash2,
  Play,
  Pause,
  RefreshCw,
  Layers,
  ArrowUpRight,
  ArrowDownLeft,
  Wifi
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ResponsiveContainer, AreaChart, Area, XAxis, YAxis, Tooltip, CartesianGrid } from "recharts"
import { type FirewallLog } from "@/data-mockup/mockData"
import { dashboardService, type DashboardStats } from "@/services/dashboardService"
import { useAlert } from "@/components/AlertDialogProvider"

// Structure for Recharts data
interface TrafficData {
  time: string
  inbound: number
  outbound: number
}



export default function Dashboard() {
  const { alert } = useAlert()
  // --- 1. Real-time Uptime & Live Time ---
  const [systemTime, setSystemTime] = useState<string>("")
  const [uptimeSeconds, setUptimeSeconds] = useState<number>(2 * 24 * 3600 + 14 * 3600 + 32 * 60 + 15) // Init at 2d 14h 32m 15s

  useEffect(() => {
    const timer = setInterval(() => {
      // Tick system time
      const now = new Date()
      setSystemTime(
        now.getFullYear() +
          "-" +
          String(now.getMonth() + 1).padStart(2, "0") +
          "-" +
          String(now.getDate()).padStart(2, "0") +
          " " +
          String(now.getHours()).padStart(2, "0") +
          ":" +
          String(now.getMinutes()).padStart(2, "0") +
          ":" +
          String(now.getSeconds()).padStart(2, "0")
      )
      // Tick uptime
      setUptimeSeconds((prev) => prev + 1)
    }, 1000)

    return () => clearInterval(timer)
  }, [])

  const formattedUptime = useMemo(() => {
    const d = Math.floor(uptimeSeconds / (24 * 3600))
    const h = Math.floor((uptimeSeconds % (24 * 3600)) / 3600)
    const m = Math.floor((uptimeSeconds % 3600) / 60)
    const s = uptimeSeconds % 60
    return `${String(d).padStart(2, "0")} วัน ${String(h).padStart(2, "0")} ชั่วโมง ${String(m).padStart(2, "0")} นาที ${String(s).padStart(2, "0")} วินาที`
  }, [uptimeSeconds])

  // --- 2. Live Performance Metrics & Stats ---
  const [cpuUsage, setCpuUsage] = useState<number>(14)
  const [memUsage, setMemUsage] = useState<number>(15)
  const [boardTemp, setBoardTemp] = useState<number>(48.5)

  const [stats, setStats] = useState<DashboardStats>({
    firewallStatus: "Active",
    totalTrafficIn: "8.7 GB",
    totalTrafficOut: "3.7 GB",
    dhcpLeasesCount: 18,
    wifiStatus: "wlan0 Master",
    wifiSSID: "PiGate-Secure",
  })

  const fetchStats = async () => {
    try {
      const data = await dashboardService.getStats()
      setStats(data)
    } catch (err) {}
  }

  useEffect(() => {
    fetchStats()
    const statsInterval = setInterval(fetchStats, 10000)
    return () => clearInterval(statsInterval)
  }, [])

  useEffect(() => {
    const fetchPerf = async () => {
      try {
        const perf = await dashboardService.getPerformanceMetrics()
        setCpuUsage(perf.cpu)
        setMemUsage(perf.memory)
        setBoardTemp(perf.temp)
      } catch (err) {}
    }

    fetchPerf() // initial fetch
    const perfInterval = setInterval(fetchPerf, 3000)
    return () => clearInterval(perfInterval)
  }, [])

  // --- 3. Bandwidth Chart Simulation ---
  const [trafficData, setTrafficData] = useState<TrafficData[]>(() => {
    // Generate initial historical points
    const base = []
    const now = new Date()
    for (let i = 14; i >= 0; i--) {
      const t = new Date(now.getTime() - i * 2000)
      const timeStr = String(t.getHours()).padStart(2, "0") + ":" +
                      String(t.getMinutes()).padStart(2, "0") + ":" +
                      String(t.getSeconds()).padStart(2, "0")
      base.push({
        time: timeStr,
        inbound: Math.round((4.0 + Math.random() * 2) * 10) / 10,
        outbound: Math.round((1.0 + Math.random() * 0.8) * 10) / 10
      })
    }
    return base
  })

  useEffect(() => {
    const trafficInterval = setInterval(() => {
      setTrafficData((prev) => {
        const t = new Date()
        const timeStr = String(t.getHours()).padStart(2, "0") + ":" +
                        String(t.getMinutes()).padStart(2, "0") + ":" +
                        String(t.getSeconds()).padStart(2, "0")

        // New speeds
        const lastIn = prev[prev.length - 1].inbound
        const lastOut = prev[prev.length - 1].outbound

        // Random walk
        const nextIn = Math.max(1.5, Math.min(45, lastIn + (Math.random() - 0.5) * 1.5))
        const nextOut = Math.max(0.4, Math.min(18, lastOut + (Math.random() - 0.5) * 0.6))

        const newPoint: TrafficData = {
          time: timeStr,
          inbound: Math.round(nextIn * 10) / 10,
          outbound: Math.round(nextOut * 10) / 10
        }

        const sliced = prev.slice(1)
        return [...sliced, newPoint]
      })
    }, 2000)

    return () => clearInterval(trafficInterval)
  }, [])

  // --- 4. Firewall Logs Streaming ---
  const [logs, setLogs] = useState<FirewallLog[]>([])
  const [isLiveStreaming, setIsLiveStreaming] = useState<boolean>(true)
  const [searchQuery, setSearchQuery] = useState<string>("")
  const [actionFilter, setActionFilter] = useState<"ALL" | "PASS" | "DROP">("ALL")

  // Load initial logs
  useEffect(() => {
    const loadLogs = async () => {
      try {
        const initialLogs = await dashboardService.getRecentLogs()
        setLogs(initialLogs)
      } catch (err) {}
    }
    loadLogs()
  }, [])

  // Simulator for SSE / Log Append
  useEffect(() => {
    if (!isLiveStreaming) return

    const logGenerator = setInterval(() => {
      const newLog = dashboardService.generateMockLog()
      setLogs((prev) => {
        const combined = [newLog, ...prev]
        return combined.slice(0, 50) // Keep at most 50 elements
      })
    }, 4500)

    return () => clearInterval(logGenerator)
  }, [isLiveStreaming])

  // Memoized filtered logs
  const filteredLogs = useMemo(() => {
    return logs.filter((log) => {
      // 1. Action Filter
      if (actionFilter !== "ALL" && log.action !== actionFilter) {
        return false
      }
      // 2. Text Search
      const query = searchQuery.trim().toLowerCase()
      if (!query) return true

      return (
        log.src.toLowerCase().includes(query) ||
        log.dest.toLowerCase().includes(query) ||
        log.port.toLowerCase().includes(query) ||
        log.proto.toLowerCase().includes(query) ||
        log.reason.toLowerCase().includes(query)
      )
    })
  }, [logs, actionFilter, searchQuery])

  // --- Helpers for color representation ---
  const getTempColor = (temp: number) => {
    if (temp < 50) return "text-primary bg-primary/10 border-primary/20"
    if (temp < 70) return "text-amber-500 bg-amber-500/10 border-amber-500/20"
    return "text-red-500 bg-red-500/10 border-red-500/20"
  }

  const getCpuColor = (cpu: number) => {
    if (cpu < 50) return "bg-primary"
    if (cpu < 85) return "bg-amber-500"
    return "bg-red-500"
  }

  const handleRefresh = async () => {
    fetchStats()
    try {
      const perf = await dashboardService.getPerformanceMetrics()
      setCpuUsage(perf.cpu)
      setMemUsage(perf.memory)
      setBoardTemp(perf.temp)
    } catch (err) {}
  }

  const handleClearLogs = async () => {
    try {
      await dashboardService.clearLogs()
      setLogs([])
    } catch (err: any) {
      await alert("ข้อผิดพลาด", "Failed to clear logs: " + err.message)
    }
  }

  return (
    <div className="space-y-6">
      {/* 1. Header Overview */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground">Dashboard Overview</h1>
          <p className="text-muted-foreground mt-1">Real-time status of your PiGate Firewall & Gateway.</p>
        </div>
        <div className="flex items-center gap-3">
          <Badge
            variant="outline"
            className="flex items-center gap-1.5 px-3 py-1 bg-primary/5 text-primary border-primary/20 animate-fade-in"
          >
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-primary opacity-75"></span>
              <span className="relative inline-flex rounded-full h-2 w-2 bg-primary"></span>
            </span>
            SSE Connected
          </Badge>

          <Button
            variant="outline"
            size="sm"
            onClick={handleRefresh}
            className="cursor-pointer gap-2"
          >
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {/* 2. Grid Stats Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {/* Card 1: Firewall Status */}
        <Card size="sm" className="bg-card/40 border border-border/60 transition duration-300 hover:border-primary/20">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Firewall Status</CardTitle>
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-primary/10 text-primary border border-primary/20">
              <Shield className="h-4 w-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1">
            <div className="flex items-center gap-2">
              <span className="text-2xl font-bold text-foreground">{stats.firewallStatus}</span>
              <span className="flex h-2.5 w-2.5 rounded-full bg-primary animate-pulse"></span>
            </div>
            <CardDescription className="text-xs text-muted-foreground">nftables kernel module active</CardDescription>
          </CardContent>
        </Card>

        {/* Card 2: Total Traffic */}
        <Card size="sm" className="bg-card/40 border border-border/60 transition duration-300 hover:border-cyan-500/20">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Total Traffic</CardTitle>
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-cyan-500/10 text-cyan-500 border border-cyan-500/20">
              <Activity className="h-4 w-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1">
            <span className="text-2xl font-bold text-foreground">12.4 GB</span>
            <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
              <span className="flex items-center text-cyan-500"><ArrowUpRight className="h-3 w-3 mr-0.5" /> {stats.totalTrafficIn} In</span>
              <span className="mx-1">•</span>
              <span className="flex items-center text-indigo-400"><ArrowDownLeft className="h-3 w-3 mr-0.5" /> {stats.totalTrafficOut} Out</span>
            </div>
          </CardContent>
        </Card>

        {/* Card 3: DHCP Leases */}
        <Card size="sm" className="bg-card/40 border border-border/60 transition duration-300 hover:border-indigo-500/20">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">DHCP Leases</CardTitle>
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-indigo-500/10 text-indigo-500 border border-indigo-500/20">
              <Users className="h-4 w-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1">
            <span className="text-2xl font-bold text-foreground">{stats.dhcpLeasesCount} Clients</span>
            <CardDescription className="text-xs text-muted-foreground">Active IP allocations (Pool: {stats.dhcpLeasesCount}/50)</CardDescription>
          </CardContent>
        </Card>

        {/* Card 4: Wi-Fi Status */}
        <Card size="sm" className="bg-card/40 border border-border/60 transition duration-300 hover:border-amber-500/20">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-1.5">
            <CardTitle className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Wi-Fi Status</CardTitle>
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-amber-500/10 text-amber-500 border border-amber-500/20">
              <Radio className="h-4 w-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1">
            <span className="text-2xl font-bold text-foreground">{stats.wifiStatus}</span>
            <CardDescription className="text-xs text-muted-foreground truncate">SSID: {stats.wifiSSID} (8 Clients)</CardDescription>
          </CardContent>
        </Card>
      </div>
      <div className="grid gap-6 lg:grid-cols-3">
        {/* Recharts Area Chart */}
        <Card className="lg:col-span-2 bg-card/25 border border-border/50 overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between border-b border-border/40 pb-4">
            <div>
              <CardTitle className="text-lg font-bold text-foreground flex items-center gap-2">
                <Activity className="h-5 w-5 text-cyan-500" />
                WAN Real-time Traffic (ความเร็วอินเทอร์เน็ต)
              </CardTitle>
              <CardDescription className="text-xs text-muted-foreground">WAN load (Inbound / Outbound interface rates)</CardDescription>
            </div>
            <div className="flex items-center gap-3 text-xs">
              <div className="flex items-center gap-1">
                <span className="h-2 w-4 rounded-full bg-cyan-400"></span>
                <span className="text-muted-foreground">Inbound</span>
              </div>
              <div className="flex items-center gap-1">
                <span className="h-2 w-4 rounded-full bg-indigo-500"></span>
                <span className="text-muted-foreground">Outbound</span>
              </div>
            </div>
          </CardHeader>
          <CardContent className="pt-6">
            <div className="h-72 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart
                  data={trafficData}
                  margin={{ top: 10, right: 10, left: -20, bottom: 0 }}
                >
                  <defs>
                    <linearGradient id="colorInbound" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#22d3ee" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="#22d3ee" stopOpacity={0} />
                    </linearGradient>
                    <linearGradient id="colorOutbound" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#6366f1" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="#6366f1" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="rgba(255,255,255,0.05)" />
                  <XAxis
                    dataKey="time"
                    stroke="#888888"
                    fontSize={10}
                    tickLine={false}
                    axisLine={false}
                  />
                  <YAxis
                    stroke="#888888"
                    fontSize={10}
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={(val) => `${val}M`}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "rgba(23, 23, 23, 0.95)",
                      borderColor: "rgba(255, 255, 255, 0.1)",
                      borderRadius: "8px",
                      color: "#ffffff"
                    }}
                    labelStyle={{ fontSize: "11px", fontWeight: "bold", color: "#a3a3a3" }}
                  />
                  <Area
                    type="monotone"
                    dataKey="inbound"
                    stroke="#22d3ee"
                    strokeWidth={2}
                    fillOpacity={1}
                    fill="url(#colorInbound)"
                    name="Inbound Speed (Mbps)"
                    isAnimationActive={false}
                  />
                  <Area
                    type="monotone"
                    dataKey="outbound"
                    stroke="#6366f1"
                    strokeWidth={2}
                    fillOpacity={1}
                    fill="url(#colorOutbound)"
                    name="Outbound Speed (Mbps)"
                    isAnimationActive={false}
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>
            <div className="mt-4 flex items-center justify-between border-t border-border/40 pt-4 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5">
                <ArrowUpRight className="h-4 w-4 text-cyan-400" />
                Inbound Current: <span className="font-semibold text-foreground">{trafficData[trafficData.length - 1]?.inbound} Mbps</span>
              </span>
              <span className="flex items-center gap-1.5">
                <ArrowDownLeft className="h-4 w-4 text-indigo-400" />
                Outbound Current: <span className="font-semibold text-foreground">{trafficData[trafficData.length - 1]?.outbound} Mbps</span>
              </span>
            </div>
          </CardContent>
        </Card>

        {/* 4. Live Firewall Logs */}
        <Card className="bg-card/25 border border-border/50 flex flex-col h-full overflow-hidden">
          <CardHeader className="border-b border-border/40 pb-4 flex flex-row items-center justify-between">
            <div>
              <CardTitle className="text-lg font-bold text-foreground flex items-center gap-2">
                <Layers className="h-5 w-5 text-indigo-400" />
                Recent Firewall Logs
              </CardTitle>
              <CardDescription className="text-xs text-muted-foreground">Live nftables packet filter logs</CardDescription>
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="icon-xs"
                onClick={() => setIsLiveStreaming(!isLiveStreaming)}
                title={isLiveStreaming ? "Pause stream" : "Resume stream"}
                className={`cursor-pointer ${isLiveStreaming ? "border-primary/20 text-primary bg-primary/5" : "text-muted-foreground"}`}
              >
                {isLiveStreaming ? <Pause className="h-3 w-3" /> : <Play className="h-3 w-3" />}
              </Button>
              <Button
                variant="outline"
                size="icon-xs"
                onClick={handleClearLogs}
                title="Clear Logs"
                className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10"
              >
                <Trash2 className="h-3 w-3" />
              </Button>
            </div>
          </CardHeader>

          {/* Interactive Filters inside Card */}
          <div className="p-4 border-b border-border/30 bg-muted/20 space-y-2.5">
            {/* Search */}
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="ค้นหา IP / Port / บริการ..."
                className="h-8.5 w-full rounded-lg border border-border bg-background/50 pl-8 pr-3 text-xs placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/50"
              />
            </div>
            {/* Action Segment Filter */}
            <div className="flex rounded-md border border-border bg-background/30 p-0.5 text-xs">
              {(["ALL", "PASS", "DROP"] as const).map((filter) => (
                <button
                  key={filter}
                  onClick={() => setActionFilter(filter)}
                  className={`flex-1 py-1 rounded text-center cursor-pointer font-medium transition ${
                    actionFilter === filter
                      ? "bg-card text-foreground font-semibold"
                      : "text-muted-foreground hover:text-foreground"
                  }`}
                >
                  {filter}
                </button>
              ))}
            </div>
          </div>

          <CardContent className="flex-1 overflow-y-auto p-4 space-y-3 max-h-[310px]">
            {filteredLogs.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-10 text-muted-foreground text-xs">
                <span>ไม่พบข้อมูล Log ในฟิลเตอร์นี้</span>
                {logs.length > 0 && (
                  <button
                    onClick={() => {
                      setSearchQuery("")
                      setActionFilter("ALL")
                    }}
                    className="text-primary mt-1 hover:underline cursor-pointer"
                  >
                    ล้างการค้นหา
                  </button>
                )}
              </div>
            ) : (
              filteredLogs.map((log) => (
                <div
                  key={log.id}
                  className="flex flex-col space-y-1.5 rounded-lg border border-border/50 bg-card/30 p-3 text-xs transition duration-200 hover:border-border"
                >
                  <div className="flex justify-between items-center">
                    <span className="text-muted-foreground font-mono">{log.time}</span>
                    <Badge
                      variant={log.action === "DROP" ? "destructive" : "outline"}
                      className={`h-4.5 rounded px-1.5 text-[9px] font-bold ${
                        log.action === "PASS"
                          ? "bg-primary/10 text-primary border border-primary/20 hover:bg-primary/20"
                          : "bg-red-500/10 text-red-500 border-red-500/20 hover:bg-red-500/20"
                      }`}
                    >
                      {log.action}
                    </Badge>
                  </div>
                  <div className="text-foreground/90 font-mono text-[11px] leading-tight">
                    <span className="text-muted-foreground">Src:</span> {log.src} <br />
                    <span className="text-muted-foreground">Dest:</span> {log.dest}:
                    <span className="text-indigo-400 font-semibold">{log.port}</span>
                    <span className="text-muted-foreground ml-1.5 font-sans">({log.proto})</span>
                  </div>
                  <div className="text-[10px] text-muted-foreground/80 pt-1 border-t border-border/20 flex justify-between">
                    <span>{log.reason}</span>
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </div>

      {/* 5. Lower Row: Details (System Info, System Performance, Interfaces) */}
      <div className="grid gap-6 md:grid-cols-3">
        {/* Widget 1: System Information */}
        <Card className="bg-card/25 border border-border/50">
          <CardHeader className="border-b border-border/40 pb-4">
            <CardTitle className="text-md font-bold text-foreground flex items-center gap-2">
              <Clock className="h-5 w-5 text-muted-foreground" />
              📌 System Information
            </CardTitle>
          </CardHeader>
          <CardContent className="pt-4">
            <div className="space-y-3 text-sm">
              <div className="flex justify-between items-center py-1.5 border-b border-border/30">
                <span className="text-muted-foreground">Hostname:</span>
                <span className="font-semibold text-foreground">PiGate-RPI5</span>
              </div>
              <div className="flex justify-between items-center py-1.5 border-b border-border/30">
                <span className="text-muted-foreground">Firmware Version:</span>
                <span className="font-semibold text-foreground">v1.0.0 (เฟสแรก)</span>
              </div>
              <div className="flex justify-between items-center py-1.5 border-b border-border/30">
                <span className="text-muted-foreground">OS Base:</span>
                <span className="font-semibold text-foreground">Raspberry Pi OS (64-bit)</span>
              </div>
              <div className="flex flex-col py-1 border-b border-border/30 space-y-1">
                <span className="text-muted-foreground text-xs">System Uptime:</span>
                <span className="font-semibold text-foreground font-mono text-[13px]">{formattedUptime}</span>
              </div>
              <div className="flex flex-col py-1 space-y-1">
                <span className="text-muted-foreground text-xs">System Time:</span>
                <span className="font-semibold text-foreground font-mono text-[13px]">{systemTime || "Loading..."}</span>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Widget 2: System Performance */}
        <Card className="bg-card/25 border border-border/50">
          <CardHeader className="border-b border-border/40 pb-4">
            <CardTitle className="text-md font-bold text-foreground flex items-center gap-2">
              <Cpu className="h-5 w-5 text-muted-foreground" />
              📈 System Performance
            </CardTitle>
          </CardHeader>
          <CardContent className="pt-4 space-y-4">
            {/* CPU */}
            <div className="space-y-1.5">
              <div className="flex justify-between items-center text-xs">
                <span className="text-muted-foreground font-medium">CPU Usage:</span>
                <span className="font-semibold text-foreground">{cpuUsage}% (4 Cores)</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={`h-full transition-all duration-500 ease-out ${getCpuColor(cpuUsage)}`}
                  style={{ width: `${cpuUsage}%` }}
                />
              </div>
            </div>

            {/* RAM */}
            <div className="space-y-1.5">
              <div className="flex justify-between items-center text-xs">
                <span className="text-muted-foreground font-medium">Memory Usage:</span>
                <span className="font-semibold text-foreground">1.2 GB / 7.6 GB ({memUsage}%)</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full bg-cyan-500 transition-all duration-500 ease-out"
                  style={{ width: `${memUsage}%` }}
                />
              </div>
            </div>

            {/* Temperature */}
            <div className="flex justify-between items-center py-2 border-b border-border/20">
              <span className="text-muted-foreground text-xs font-medium flex items-center gap-1">
                <Thermometer className="h-4 w-4" /> CPU Temperature:
              </span>
              <Badge variant="outline" className={`font-semibold text-xs ${getTempColor(boardTemp)}`}>
                {boardTemp} °C
              </Badge>
            </div>

            {/* Storage */}
            <div className="space-y-1.5">
              <div className="flex justify-between items-center text-xs">
                <span className="text-muted-foreground font-medium flex items-center gap-1">
                  <HardDrive className="h-3.5 w-3.5" /> Storage (/):
                </span>
                <span className="font-semibold text-foreground">24 GB / 58 GB (41%)</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full bg-indigo-500 transition-all duration-500 ease-out"
                  style={{ width: "41%" }}
                />
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Widget 3: Interface Status */}
        <Card className="bg-card/25 border border-border/50">
          <CardHeader className="border-b border-border/40 pb-4">
            <CardTitle className="text-md font-bold text-foreground flex items-center gap-2">
              <Wifi className="h-5 w-5 text-muted-foreground" />
              🌐 Interface Status
            </CardTitle>
          </CardHeader>
          <CardContent className="pt-4 space-y-4">
            {/* Interface 1: wlan0 */}
            <div className="rounded-lg border border-border/40 bg-card/20 p-3 space-y-2">
              <div className="flex justify-between items-center">
                <span className="font-bold text-foreground flex items-center gap-1.5">
                  <Radio className="h-4 w-4 text-amber-500" />
                  wlan0 (WAN)
                </span>
                <Badge className="bg-primary/10 text-primary border border-primary/20 hover:bg-primary/20 px-1.5 h-4.5 rounded text-[9px] font-bold">
                  Connected
                </Badge>
              </div>
              <div className="text-xs space-y-1.5 font-mono text-muted-foreground">
                <div className="flex justify-between">
                  <span>IP Address:</span>
                  <span className="text-foreground font-medium">10.0.0.45</span>
                </div>
                <div className="flex justify-between">
                  <span>SSID:</span>
                  <span className="text-foreground font-medium italic">MyHome_5G</span>
                </div>
                <div className="flex justify-between">
                  <span>Signal:</span>
                  <span className="text-primary font-semibold">84% (-58 dBm)</span>
                </div>
              </div>
            </div>

            {/* Interface 2: eth0 */}
            <div className="rounded-lg border border-border/40 bg-card/20 p-3 space-y-2">
              <div className="flex justify-between items-center">
                <span className="font-bold text-foreground flex items-center gap-1.5">
                  <Activity className="h-4 w-4 text-cyan-400" />
                  eth0 (LAN)
                </span>
                <Badge className="bg-primary/10 text-primary border border-primary/20 hover:bg-primary/20 px-1.5 h-4.5 rounded text-[9px] font-bold">
                  Link Up
                </Badge>
              </div>
              <div className="text-xs space-y-1.5 font-mono text-muted-foreground">
                <div className="flex justify-between">
                  <span>IP Address:</span>
                  <span className="text-foreground font-medium">192.168.1.1</span>
                </div>
                <div className="flex justify-between">
                  <span>Speed:</span>
                  <span className="text-foreground font-medium">1000 Mbps</span>
                </div>
                <div className="flex justify-between">
                  <span>Mode:</span>
                  <span className="text-foreground font-medium">Full Duplex</span>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 6. Integration Guide Footer */}
      <div className="rounded-xl border border-dashed border-amber-500/30 bg-amber-500/5 p-4.5 text-xs leading-relaxed space-y-2.5">
        <h4 className="font-bold text-amber-500 flex items-center gap-1.5">
          🧠 Backend Integration สําหรับ หน้า Dashboard (กลไกการดึงข้อมูล Real-time)
        </h4>
        <p className="text-muted-foreground">
          เพื่อให้หน้า Dashboard แสดงผลแบบขยับได้สดๆ บนหน้าเว็บ แนะนำให้เขียนฝั่ง Frontend ยิงคำสั่งดึงข้อมูล (Polling API) มาอัปเดตทุกๆ 3-5 วินาที โดย Backend (Go) จะใช้ช่องทางของคอร์ระบบในการไปแกะข้อมูลมาจัดฟอร์แมต JSON หรือสื่อสารผ่าน Server-Sent Events (SSE):
        </p>
        <pre className="overflow-x-auto rounded-lg bg-neutral-900 dark:bg-black p-3.5 font-mono text-[11px] text-neutral-300 border border-neutral-800">
          {"// 1. การดึงค่า CPU / RAM / Temp (ใช้ `/sys/class/` หรือ `gopsutil` ใน Go Backend)\n" +
           "// ดึงความร้อนบอร์ด RPI5:\n" +
           "// Reading /sys/class/thermal/thermal_zone0/temp -> Divide by 1000\n\n" +
           "// 2. การดึงประวัติการบล็อกคัดกรองแพ็กเก็ตจาก nftables มาแสดงในตาราง Log\n" +
           "// ต้องเปิดการทำ log ในกฎ nftables ก่อน (เช่น nft add rule ip filter FORWARD log prefix \"PiGate_Drop: \" drop)\n" +
           "// จากนั้นอ่าน log สดผ่าน D-Bus หรือ tail dmesg / journald logs"}
        </pre>
      </div>
    </div>
  )
}
