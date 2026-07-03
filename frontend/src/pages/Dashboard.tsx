import { useState, useEffect, useMemo, useRef } from "react"
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
  Wifi,
  Network,
  Zap,
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ResponsiveContainer, AreaChart, Area, XAxis, YAxis, Tooltip, CartesianGrid } from "recharts"
import { type FirewallLog } from "@/data-mockup/mockData"
import { dashboardService, type DashboardStats } from "@/services/dashboardService"
import { interfaceService } from "@/services/interfaceService"
import { systemService } from "@/services/systemService"
import { type NetworkInterface } from "@/data-mockup/mockData"
import { useAlert } from "@/components/AlertDialogProvider"
import { useTheme } from "@/components/ThemeProvider"

// Structure for Recharts data
interface TrafficData {
  time: string
  inbound: number
  outbound: number
}

// SSE connection status
type SSEStatus = "connecting" | "connected" | "disconnected" | "mock"

export default function Dashboard() {
  const { alert } = useAlert()
  const { theme } = useTheme()

  // --- 1. Real-time Uptime & Live Time ---
  const [systemTime, setSystemTime] = useState<string>("")
  const [uptimeSeconds, setUptimeSeconds] = useState<number>(0)

  useEffect(() => {
    const timer = setInterval(() => {
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
      setUptimeSeconds((prev) => prev + 1)
    }, 1000)

    return () => clearInterval(timer)
  }, [])

  const formattedUptime = useMemo(() => {
    if (uptimeSeconds === 0) return "กำลังโหลด..."
    const d = Math.floor(uptimeSeconds / (24 * 3600))
    const h = Math.floor((uptimeSeconds % (24 * 3600)) / 3600)
    const m = Math.floor((uptimeSeconds % 3600) / 60)
    const s = uptimeSeconds % 60
    if (d > 0) {
      return `${d} วัน ${String(h).padStart(2, "0")} ชั่วโมง ${String(m).padStart(2, "0")} นาที`
    }
    return `${String(h).padStart(2, "0")} ชั่วโมง ${String(m).padStart(2, "0")} นาที ${String(s).padStart(2, "0")} วินาที`
  }, [uptimeSeconds])

  // --- 2. Live Performance Metrics & Stats ---
  const [cpuUsage, setCpuUsage] = useState<number>(0)
  const [memUsage, setMemUsage] = useState<number>(0)
  const [boardTemp, setBoardTemp] = useState<number>(0)
  const [isLoadingPerf, setIsLoadingPerf] = useState(true)

  const [stats, setStats] = useState<DashboardStats>({
    firewallStatus: "Loading...",
    totalTrafficIn: "—",
    totalTrafficOut: "—",
    dhcpLeasesCount: 0,
    wifiStatus: "—",
    wifiSSID: "—",
  })

  const fetchStats = async () => {
    try {
      const data = await dashboardService.getStats()
      setStats(data)
    } catch (err) { }
  }

  useEffect(() => {
    fetchStats()
    const statsInterval = setInterval(fetchStats, 10000)
    return () => clearInterval(statsInterval)
  }, [])

  // --- Device Hostname (System Information card) ---
  const [hostname, setHostname] = useState<string>("PiGate-RPI5")

  useEffect(() => {
    const fetchHostname = async () => {
      try {
        const data = await systemService.getHostname()
        setHostname(data.hostname)
      } catch {
        // keep the fallback hostname already in state
      }
    }
    fetchHostname()
  }, [])

  useEffect(() => {
    const fetchPerf = async () => {
      try {
        const perf = await dashboardService.getPerformanceMetrics()
        setCpuUsage(perf.cpu)
        setMemUsage(perf.memory)
        setBoardTemp(perf.temp)
        setIsLoadingPerf(false)
      } catch (err) {
        setIsLoadingPerf(false)
      }
    }

    fetchPerf() // initial fetch
    const perfInterval = setInterval(fetchPerf, 3000)
    return () => clearInterval(perfInterval)
  }, [])

  // --- 3. Network Interfaces from API ---
  const [interfaces, setInterfaces] = useState<NetworkInterface[]>([])
  const [isLoadingInterfaces, setIsLoadingInterfaces] = useState(true)

  useEffect(() => {
    const loadInterfaces = async () => {
      try {
        const data = await interfaceService.getAll()
        setInterfaces(data)
      } catch (err) {
        console.error("Failed to load interfaces:", err)
      } finally {
        setIsLoadingInterfaces(false)
      }
    }
    loadInterfaces()
  }, [])

  const handleRefreshInterfaces = async () => {
    setIsLoadingInterfaces(true)
    try {
      const data = await interfaceService.getAll()
      setInterfaces(data)
    } catch (err) { }
    setIsLoadingInterfaces(false)
  }

  // --- 4. Bandwidth Chart Simulation ---
  const [trafficData, setTrafficData] = useState<TrafficData[]>(() => {
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

        const lastIn = prev[prev.length - 1].inbound
        const lastOut = prev[prev.length - 1].outbound
        const nextIn = Math.max(1.5, Math.min(45, lastIn + (Math.random() - 0.5) * 1.5))
        const nextOut = Math.max(0.4, Math.min(18, lastOut + (Math.random() - 0.5) * 0.6))

        const newPoint: TrafficData = {
          time: timeStr,
          inbound: Math.round(nextIn * 10) / 10,
          outbound: Math.round(nextOut * 10) / 10
        }

        return [...prev.slice(1), newPoint]
      })
    }, 2000)

    return () => clearInterval(trafficInterval)
  }, [])

  // --- 5. Firewall Logs via SSE ---
  const [logs, setLogs] = useState<FirewallLog[]>([])
  const [isLiveStreaming, setIsLiveStreaming] = useState<boolean>(true)
  const [sseStatus, setSseStatus] = useState<SSEStatus>("connecting")
  const [searchQuery, setSearchQuery] = useState<string>("")
  const [actionFilter, setActionFilter] = useState<"ALL" | "PASS" | "DROP">("ALL")
  const cleanupSSERef = useRef<(() => void) | null>(null)

  // Load initial logs
  useEffect(() => {
    const loadLogs = async () => {
      try {
        const initialLogs = await dashboardService.getRecentLogs()
        setLogs(initialLogs)
      } catch (err) { }
    }
    loadLogs()
  }, [])

  // SSE stream connection
  useEffect(() => {
    if (!isLiveStreaming) {
      // Disconnect SSE
      if (cleanupSSERef.current) {
        cleanupSSERef.current()
        cleanupSSERef.current = null
      }
      setSseStatus("disconnected")
      return
    }

    setSseStatus("connecting")

    const cleanup = dashboardService.connectSSELogs(
      (newLog) => {
        setSseStatus(import.meta.env.DEV ? "mock" : "connected")
        setLogs((prev) => {
          const combined = [newLog, ...prev]
          return combined.slice(0, 50)
        })
      },
      (_err) => {
        setSseStatus("disconnected")
      }
    )

    // For mock mode: set status after a short delay
    const statusTimer = setTimeout(() => {
      setSseStatus((prev) => prev === "connecting" ? "mock" : prev)
    }, 600)

    cleanupSSERef.current = cleanup

    return () => {
      clearTimeout(statusTimer)
      cleanup()
      cleanupSSERef.current = null
    }
  }, [isLiveStreaming])

  // Memoized filtered logs
  const filteredLogs = useMemo(() => {
    return logs.filter((log) => {
      if (actionFilter !== "ALL" && log.action !== actionFilter) return false
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

  // --- Helpers ---
  const getTempColor = (temp: number) => {
    if (temp === 0) return "text-muted-foreground bg-muted/30 border-border/40"
    if (temp < 50) return "text-primary bg-primary/10 border-primary/20"
    if (temp < 70) return "text-amber-500 bg-amber-500/10 border-amber-500/20"
    return "text-red-500 bg-red-500/10 border-red-500/20"
  }

  const getCpuColor = (cpu: number) => {
    if (cpu < 50) return "bg-primary"
    if (cpu < 85) return "bg-amber-500"
    return "bg-red-500"
  }

  const getInterfaceIcon = (iface: NetworkInterface) => {
    if (iface.type === "wireless") return <Radio className="h-4 w-4 text-amber-500" />
    return <Activity className="h-4 w-4 text-cyan-400" />
  }

  const getInterfaceRoleColor = (role: string) => {
    if (role === "WAN") return "text-amber-500"
    return "text-cyan-400"
  }

  const chartTooltipStyle = {
    backgroundColor: theme === "dark" ? "rgba(23, 23, 23, 0.95)" : "rgba(255, 255, 255, 0.97)",
    borderColor: theme === "dark" ? "rgba(255, 255, 255, 0.1)" : "rgba(0,0,0,0.1)",
    borderRadius: "8px",
    color: theme === "dark" ? "#ffffff" : "#111111",
  }

  const gridStroke = theme === "dark" ? "rgba(255,255,255,0.05)" : "rgba(0,0,0,0.06)"

  const handleRefresh = async () => {
    fetchStats()
    try {
      const perf = await dashboardService.getPerformanceMetrics()
      setCpuUsage(perf.cpu)
      setMemUsage(perf.memory)
      setBoardTemp(perf.temp)
    } catch (err) { }
    handleRefreshInterfaces()
  }

  const handleClearLogs = async () => {
    try {
      await dashboardService.clearLogs()
      setLogs([])
    } catch (err: any) {
      await alert("ข้อผิดพลาด", "Failed to clear logs: " + err.message)
    }
  }

  // SSE status badge config
  const sseStatusConfig: Record<SSEStatus, { label: string; className: string }> = {
    connected: {
      label: "SSE Connected",
      className: "bg-primary/5 text-primary border-primary/20",
    },
    mock: {
      label: "Live (Simulated)",
      className: "bg-cyan-500/5 text-cyan-500 border-cyan-500/20",
    },
    connecting: {
      label: "Connecting...",
      className: "bg-amber-500/5 text-amber-500 border-amber-500/20",
    },
    disconnected: {
      label: "Stream Paused",
      className: "bg-muted/40 text-muted-foreground border-border/40",
    },
  }

  const currentSseConfig = sseStatusConfig[sseStatus]

  return (
    <div className="space-y-6">
      {/* 1. Header Overview */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground">Dashboard Overview</h1>
          <p className="text-muted-foreground mt-1">Real-time status of your PiGate Firewall &amp; Gateway.</p>
        </div>
        <div className="flex items-center gap-3">
          <Badge
            variant="outline"
            className={`flex items-center gap-1.5 px-3 py-1 animate-fade-in ${currentSseConfig.className}`}
          >
            {isLiveStreaming && (
              <span className="relative flex h-2 w-2">
                <span className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${sseStatus === "connected" ? "bg-primary" : sseStatus === "mock" ? "bg-cyan-500" : "bg-amber-500"}`}></span>
                <span className={`relative inline-flex rounded-full h-2 w-2 ${sseStatus === "connected" ? "bg-primary" : sseStatus === "mock" ? "bg-cyan-500" : "bg-amber-500"}`}></span>
              </span>
            )}
            {currentSseConfig.label}
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
              {stats.firewallStatus === "Active" && (
                <span className="flex h-2.5 w-2.5 rounded-full bg-primary animate-pulse"></span>
              )}
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
            <span className="text-2xl font-bold text-foreground">
              {stats.totalTrafficIn !== "—" ? (
                <>
                  {/* Sum if both available, else show individual */}
                  {stats.totalTrafficIn}
                </>
              ) : "—"}
            </span>
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
            <CardDescription className="text-xs text-muted-foreground">Active IP allocations</CardDescription>
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
            <CardDescription className="text-xs text-muted-foreground truncate">SSID: {stats.wifiSSID}</CardDescription>
          </CardContent>
        </Card>
      </div>

      {/* 3. Traffic Chart + Firewall Logs */}
      <div className="grid gap-6 lg:grid-cols-3">
        {/* Recharts Area Chart */}
        <Card className="lg:col-span-2 bg-card/25 border border-border/50 overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between border-b border-border/40 pb-4">
            <div>
              <CardTitle className="text-lg font-bold text-foreground flex items-center gap-2">
                <Activity className="h-5 w-5 text-cyan-500" />
                WAN Real-time Traffic
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
                  <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={gridStroke} />
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
                    contentStyle={chartTooltipStyle}
                    labelStyle={{ fontSize: "11px", fontWeight: "bold", color: theme === "dark" ? "#a3a3a3" : "#666" }}
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
                Inbound: <span className="font-semibold text-foreground">{trafficData[trafficData.length - 1]?.inbound} Mbps</span>
              </span>
              <span className="flex items-center gap-1.5">
                <ArrowDownLeft className="h-4 w-4 text-indigo-400" />
                Outbound: <span className="font-semibold text-foreground">{trafficData[trafficData.length - 1]?.outbound} Mbps</span>
              </span>
            </div>
          </CardContent>
        </Card>

        {/* Live Firewall Logs */}
        <Card className="bg-card/25 border border-border/50 flex flex-col h-full overflow-hidden">
          <CardHeader className="border-b border-border/40 pb-4 flex flex-row items-center justify-between">
            <div>
              <CardTitle className="text-lg font-bold text-foreground flex items-center gap-2">
                <Layers className="h-5 w-5 text-indigo-400" />
                Firewall Logs
              </CardTitle>
              <CardDescription className="text-xs text-muted-foreground">Live nftables packet filter events</CardDescription>
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

          {/* Filters */}
          <div className="p-4 border-b border-border/30 bg-muted/20 space-y-2.5">
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
            <div className="flex rounded-md border border-border bg-background/30 p-0.5 text-xs">
              {(["ALL", "PASS", "DROP"] as const).map((filter) => (
                <button
                  key={filter}
                  onClick={() => setActionFilter(filter)}
                  className={`flex-1 py-1 rounded text-center cursor-pointer font-medium transition ${actionFilter === filter
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
                    onClick={() => { setSearchQuery(""); setActionFilter("ALL") }}
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
                      className={`h-4.5 rounded px-1.5 text-[9px] font-bold ${log.action === "PASS"
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

      {/* 4. Lower Row: System Info, Performance, Interfaces */}
      <div className="grid gap-6 md:grid-cols-3">
        {/* Widget 1: System Information */}
        <Card className="bg-card/25 border border-border/50">
          <CardHeader className="border-b border-border/40 pb-4">
            <CardTitle className="text-md font-bold text-foreground flex items-center gap-2">
              <Clock className="h-5 w-5 text-muted-foreground" />
              System Information
            </CardTitle>
          </CardHeader>
          <CardContent className="pt-4">
            <div className="space-y-3 text-sm">
              <div className="flex justify-between items-center py-1.5 border-b border-border/30">
                <span className="text-muted-foreground">Hostname:</span>
                <span className="font-semibold text-foreground">{hostname}</span>
              </div>
              <div className="flex justify-between items-center py-1.5 border-b border-border/30">
                <span className="text-muted-foreground">Firmware:</span>
                <span className="font-semibold text-foreground flex items-center gap-1.5">
                  <Zap className="h-3.5 w-3.5 text-primary" />
                  v1.0.0
                </span>
              </div>
              <div className="flex justify-between items-center py-1.5 border-b border-border/30">
                <span className="text-muted-foreground">OS Base:</span>
                <span className="font-semibold text-foreground text-right text-xs">Raspberry Pi OS (64-bit)</span>
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
              System Performance
            </CardTitle>
          </CardHeader>
          <CardContent className="pt-4 space-y-4">
            {/* CPU */}
            <div className="space-y-1.5">
              <div className="flex justify-between items-center text-xs">
                <span className="text-muted-foreground font-medium">CPU Usage:</span>
                <span className="font-semibold text-foreground">
                  {isLoadingPerf ? "—" : `${cpuUsage}%`}
                </span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={`h-full transition-all duration-500 ease-out ${getCpuColor(cpuUsage)}`}
                  style={{ width: isLoadingPerf ? "0%" : `${cpuUsage}%` }}
                />
              </div>
            </div>

            {/* RAM */}
            <div className="space-y-1.5">
              <div className="flex justify-between items-center text-xs">
                <span className="text-muted-foreground font-medium">Memory Usage:</span>
                <span className="font-semibold text-foreground">
                  {isLoadingPerf ? "—" : `${memUsage}%`}
                </span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full bg-cyan-500 transition-all duration-500 ease-out"
                  style={{ width: isLoadingPerf ? "0%" : `${memUsage}%` }}
                />
              </div>
            </div>

            {/* Temperature */}
            <div className="flex justify-between items-center py-2 border-b border-border/20">
              <span className="text-muted-foreground text-xs font-medium flex items-center gap-1">
                <Thermometer className="h-4 w-4" /> CPU Temperature:
              </span>
              <Badge variant="outline" className={`font-semibold text-xs ${getTempColor(boardTemp)}`}>
                {isLoadingPerf ? "—" : `${boardTemp} °C`}
              </Badge>
            </div>

            {/* Storage (static label — Backend doesn't expose disk yet) */}
            <div className="space-y-1.5">
              <div className="flex justify-between items-center text-xs">
                <span className="text-muted-foreground font-medium flex items-center gap-1">
                  <HardDrive className="h-3.5 w-3.5" /> Storage (/):
                </span>
                <span className="font-semibold text-foreground text-muted-foreground text-xs italic">N/A</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div className="h-full bg-indigo-500/30 transition-all duration-500 ease-out" style={{ width: "0%" }} />
              </div>
              <p className="text-[10px] text-muted-foreground/60">Disk telemetry coming soon</p>
            </div>
          </CardContent>
        </Card>

        {/* Widget 3: Interface Status (from API) */}
        <Card className="bg-card/25 border border-border/50">
          <CardHeader className="border-b border-border/40 pb-4 flex flex-row items-center justify-between">
            <CardTitle className="text-md font-bold text-foreground flex items-center gap-2">
              <Network className="h-5 w-5 text-muted-foreground" />
              Interface Status
            </CardTitle>
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={handleRefreshInterfaces}
              title="Refresh Interfaces"
              className="text-muted-foreground hover:text-foreground cursor-pointer"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${isLoadingInterfaces ? "animate-spin" : ""}`} />
            </Button>
          </CardHeader>
          <CardContent className="pt-4 space-y-3 overflow-y-auto max-h-[300px]">
            {isLoadingInterfaces ? (
              <div className="space-y-3">
                {[1, 2].map((i) => (
                  <div key={i} className="rounded-lg border border-border/30 bg-card/20 p-3 space-y-2 animate-pulse">
                    <div className="h-4 bg-muted rounded w-3/4" />
                    <div className="h-3 bg-muted rounded w-1/2" />
                  </div>
                ))}
              </div>
            ) : interfaces.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-8 text-muted-foreground text-xs">
                <Wifi className="h-8 w-8 mb-2 opacity-30" />
                <span>ไม่พบ Interface</span>
              </div>
            ) : (
              interfaces.map((iface) => (
                <div
                  key={iface.id}
                  className="rounded-lg border border-border/40 bg-card/20 p-3 space-y-2"
                >
                  <div className="flex justify-between items-center">
                    <span className={`font-bold text-foreground flex items-center gap-1.5 text-sm`}>
                      {getInterfaceIcon(iface)}
                      <span>{iface.name}</span>
                      <span className={`text-xs font-normal ${getInterfaceRoleColor(iface.role)}`}>({iface.role})</span>
                    </span>
                    <Badge
                      className={`px-1.5 h-4.5 rounded text-[9px] font-bold ${
                        iface.status === "up"
                          ? "bg-primary/10 text-primary border border-primary/20 hover:bg-primary/20"
                          : "bg-red-500/10 text-red-500 border-red-500/20 hover:bg-red-500/20"
                      }`}
                    >
                      {iface.status === "up" ? "UP" : "DOWN"}
                    </Badge>
                  </div>
                  <div className="text-xs space-y-1 font-mono text-muted-foreground">
                    <div className="flex justify-between">
                      <span>IP:</span>
                      <span className="text-foreground font-medium">
                        {iface.ip || <span className="italic text-muted-foreground/60">DHCP</span>}
                      </span>
                    </div>
                    {iface.type === "wireless" && iface.wifiSSID && (
                      <div className="flex justify-between">
                        <span>SSID:</span>
                        <span className="text-foreground font-medium italic">{iface.wifiSSID}</span>
                      </div>
                    )}
                    {iface.alias && iface.alias !== iface.name && (
                      <div className="flex justify-between">
                        <span>Alias:</span>
                        <span className="text-foreground/80 font-sans">{iface.alias}</span>
                      </div>
                    )}
                    {iface.type === "ethernet" && iface.speed && (
                      <div className="flex justify-between">
                        <span>Speed:</span>
                        <span className="text-foreground font-medium">{iface.speed}</span>
                      </div>
                    )}
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
