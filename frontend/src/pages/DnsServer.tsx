import { useState, useMemo, useEffect } from "react"
import { Link, useSearchParams } from "react-router"
import { getErrorMessage } from "@/lib/errors"
import {
  Globe,
  Plus,
  Search,
  Edit,
  Trash2,
  AlertCircle,
  RefreshCw,
  Check,
  CheckCircle2,
  Info,
  Server,
  Database,
  Loader2,
  Network,
  Ban,
  ShieldAlert,
  Download,
  Upload
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { CapabilityBanner } from "@/components/CapabilityBanner"
import {
  type DNSZone,
  type DNSRecord,
  type NetworkInterface,
  type BlockedDomain,
  type DNSBlocklist,
  DNS_CACHE_TTL_MIN,
  DNS_CACHE_TTL_MAX,
  DNS_CACHE_TTL_DEFAULT,
  DNS_CACHE_ENTRIES_MIN,
  DNS_CACHE_ENTRIES_MAX,
  DNS_CACHE_ENTRIES_DEFAULT,
  DNS_UPSTREAM_MAX_SERVERS,
  DNS_BLOCKED_DOMAINS_MAX,
  DNS_BLOCKLISTS_MAX,
  DNS_BLOCKLIST_MAX_FILE_MB,
  DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS,
} from "@/data-mockup/mockData"
import { dnsServerService } from "@/services/dnsServerService"
import { interfaceService } from "@/services/interfaceService"
import { systemService, type DNSConfig } from "@/services/systemService"
import { useAlert } from "@/hooks/useAlert"
import { isValidIp } from "@/lib/utils"
import { ifaceLabel } from "@/lib/ifaceLabel"

// Must match backend model.DNSBlocklistMaxTotalDomains (docs/ref/todo/
// dns-blocklist-import-plan.md §2.1.4). Not re-exported from mockData.ts by
// T-11 (only the per-list/upload/nxdomain caps are), so it is mirrored here
// for the Blocklists tab's summary bar only.
const DNS_BLOCKLIST_TOTAL_DOMAINS_MAX = 500000

export default function DnsServer() {
  const { alert, confirm } = useAlert()

  // ?tab= deep-link support (docs/ref/todo/statistics-nav-restructure-plan.md
  // T-08): lets the standalone DNS Statistics page's empty state link
  // straight to /network/dns-server?tab=settings, where queryLogging lives.
  // Whitelisted to the three remaining tabs — any other value (including the
  // now-dead "stats") falls back to "zones". activeTab stays the single
  // source of truth for <Tabs value=...>; the param only seeds its initial
  // value and is kept in sync on every tab switch below.
  const VALID_TABS = ["zones", "blocked", "blocklists", "settings"] as const
  type DnsServerTab = (typeof VALID_TABS)[number]
  const [searchParams, setSearchParams] = useSearchParams()
  const initialTab = searchParams.get("tab")
  const [activeTab, setActiveTabState] = useState<DnsServerTab>(
    VALID_TABS.includes(initialTab as DnsServerTab) ? (initialTab as DnsServerTab) : "zones"
  )
  const setActiveTab = (tab: string) => {
    const next = VALID_TABS.includes(tab as DnsServerTab) ? (tab as DnsServerTab) : "zones"
    setActiveTabState(next)
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev)
        params.set("tab", next)
        return params
      },
      { replace: true }
    )
  }

  // --- State ---
  const [zones, setZones] = useState<DNSZone[]>([])
  const [selectedZoneId, setSelectedZoneId] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  // Search queries
  const [zoneSearchQuery, setZoneSearchQuery] = useState("")
  const [recordSearchQuery, setRecordSearchQuery] = useState("")

  // Modals state
  const [isZoneModalOpen, setIsZoneModalOpen] = useState(false)
  const [editingZone, setEditingZone] = useState<DNSZone | null>(null)

  const [isRecModalOpen, setIsRecModalOpen] = useState(false)
  const [editingRecord, setEditingRecord] = useState<DNSRecord | null>(null)

  // Form states - Zone Modal
  const [zoneName, setZoneName] = useState("")
  const [isAuthoritative, setIsAuthoritative] = useState(true)
  const [forwardTo, setForwardTo] = useState("")
  const [allowedIps, setAllowedIps] = useState("any")
  const [zoneError, setZoneError] = useState("")

  // Form states - Record Modal
  const [recName, setRecName] = useState("")
  const [recType, setRecType] = useState("A")
  const [recValue, setRecValue] = useState("")
  const [recTtl, setRecTtl] = useState("300")
  const [recError, setRecError] = useState("")

  // Blocked Domains (deny-list, docs/ref/todo/dns-blocked-domains-plan.md)
  const [blockedDomains, setBlockedDomains] = useState<BlockedDomain[]>([])
  const [blockedSearchQuery, setBlockedSearchQuery] = useState("")
  const [isBlockedModalOpen, setIsBlockedModalOpen] = useState(false)
  const [editingBlockedDomain, setEditingBlockedDomain] = useState<BlockedDomain | null>(null)
  const [blkDomain, setBlkDomain] = useState("")
  const [blkMode, setBlkMode] = useState<"nxdomain" | "sinkhole">("nxdomain")
  const [blkComment, setBlkComment] = useState("")
  const [blkEnabled, setBlkEnabled] = useState(true)
  const [blkError, setBlkError] = useState("")
  const [isSavingBlocked, setIsSavingBlocked] = useState(false)

  // DNS Blocklists (bulk subscribe-URL/upload hosts import,
  // docs/ref/todo/dns-blocklist-import-plan.md) — distinct feature from the
  // deny-list above. blstXxx fields belong to the Add/Edit dialog form.
  const [blocklists, setBlocklists] = useState<DNSBlocklist[]>([])
  const [isBlocklistModalOpen, setIsBlocklistModalOpen] = useState(false)
  const [editingBlocklist, setEditingBlocklist] = useState<DNSBlocklist | null>(null)
  const [blstSourceType, setBlstSourceType] = useState<"url" | "upload">("url")
  const [blstName, setBlstName] = useState("")
  const [blstUrl, setBlstUrl] = useState("")
  // Default "sinkhole" — opposite of the deny-list's blkMode default above
  // (see mockData.ts DNSBlocklist.blockMode doc comment for why).
  const [blstMode, setBlstMode] = useState<"sinkhole" | "nxdomain">("sinkhole")
  const [blstEnabled, setBlstEnabled] = useState(true)
  const [blstFile, setBlstFile] = useState<File | null>(null)
  const [blstError, setBlstError] = useState("")
  const [isSavingBlocklist, setIsSavingBlocklist] = useState(false)
  // Ids currently mid-flight for a row action (refresh/toggle/delete) — a
  // refresh of a large hosts file can take several seconds, so this both
  // shows a spinner and blocks double-clicks on that row.
  const [blocklistBusyIds, setBlocklistBusyIds] = useState<Set<string>>(new Set())

  // Apply & Save states
  const [isApplying, setIsApplying] = useState(false)
  const [isApplied, setIsApplied] = useState(true)
  const [isClearingCache, setIsClearingCache] = useState(false)
  const [isSaving, setIsSaving] = useState(false)

  // Listen Interfaces state — real interfaces from Interface Service, independent of DHCP Server
  const [availableInterfaces, setAvailableInterfaces] = useState<NetworkInterface[]>([])
  const [selectedInterfaces, setSelectedInterfaces] = useState<string[]>([])
  const [isSavingInterfaces, setIsSavingInterfaces] = useState(false)

  // DNS Statistics state (docs/ref/todo/statistics-dns-top-domain-plan.md T-13):
  // queryLogging restarts dnsmasq when changed; the TTL/cap inputs are kept as
  // strings while editing (so an in-progress edit like "" or "14" isn't forced
  // back to a number every keystroke) and only parsed/validated on save.
  const [queryLogging, setQueryLogging] = useState(false)
  const [isSavingQueryLogging, setIsSavingQueryLogging] = useState(false)
  const [dnsCacheTtlInput, setDnsCacheTtlInput] = useState(String(DNS_CACHE_TTL_DEFAULT))
  const [dnsCacheMaxInput, setDnsCacheMaxInput] = useState(String(DNS_CACHE_ENTRIES_DEFAULT))
  const [dnsCacheLimitsError, setDnsCacheLimitsError] = useState("")
  const [isSavingCacheLimits, setIsSavingCacheLimits] = useState(false)

  // Upstream Resolvers state (docs/ref/todo/
  // dns-server-settings-tab-and-upstream-plan.md T-12): "system" (default)
  // shows a read-only preview of what System DNS would resolve to right now;
  // "custom" edits up to DNS_UPSTREAM_MAX_SERVERS bare IP entries directly.
  const [upstreamMode, setUpstreamMode] = useState<"system" | "custom">("system")
  const [upstreamServers, setUpstreamServers] = useState<string[]>([])
  const [systemDnsConfig, setSystemDnsConfig] = useState<DNSConfig | null>(null)
  const [upstreamError, setUpstreamError] = useState("")
  const [isSavingUpstream, setIsSavingUpstream] = useState(false)

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body.
    // selectedZoneId is still its initial null value on this first-mount load.
    const initialLoad = async () => {
      try {
        const [data, ifaces, settings, sysDns, blocked, blocklistsData] = await Promise.all([
          dnsServerService.getZones(),
          interfaceService.getAll(),
          dnsServerService.getSettings(),
          systemService.getDNSConfig(),
          dnsServerService.getBlockedDomains(),
          dnsServerService.getBlocklists(),
        ])
        setZones(data || [])
        setBlockedDomains(blocked || [])
        setBlocklists(blocklistsData || [])
        if ((data || []).length > 0) {
          setSelectedZoneId(data[0].id)
        }
        setAvailableInterfaces((ifaces || []).filter(i => i.role === "LAN"))
        setSelectedInterfaces(settings.interfaces || [])
        setQueryLogging(settings.queryLogging || false)
        setDnsCacheTtlInput(String(settings.dnsCacheTtlMinutes || DNS_CACHE_TTL_DEFAULT))
        setDnsCacheMaxInput(String(settings.dnsCacheMaxEntries || DNS_CACHE_ENTRIES_DEFAULT))
        setUpstreamMode(settings.upstreamMode || "system")
        setUpstreamServers(settings.upstreamServers || [])
        setSystemDnsConfig(sysDns)
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูล DNS Server ได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

  // --- Handlers: Listen Interfaces ---
  const handleToggleInterface = async (name: string, checked: boolean) => {
    const next = checked
      ? [...selectedInterfaces, name]
      : selectedInterfaces.filter(n => n !== name)

    setIsSavingInterfaces(true)
    try {
      // PUT /api/dns/settings takes the full settings object (not a partial
      // patch) — always send the current queryLogging/TTL/cap alongside the
      // changed interfaces so they aren't reset to their zero value.
      await dnsServerService.updateSettings({
        interfaces: next,
        queryLogging,
        dnsCacheTtlMinutes: Number(dnsCacheTtlInput) || DNS_CACHE_TTL_DEFAULT,
        dnsCacheMaxEntries: Number(dnsCacheMaxInput) || DNS_CACHE_ENTRIES_DEFAULT,
        upstreamMode,
        upstreamServers,
      })
      setSelectedInterfaces(next)
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถบันทึก Interface ของ DNS Server ได้: " + getErrorMessage(err))
    } finally {
      setIsSavingInterfaces(false)
    }
  }

  // --- Handlers: DNS Statistics (docs/ref/todo/statistics-dns-top-domain-plan.md T-13) ---
  // Toggling queryLogging restarts dnsmasq (backend ApplyAll) — warn in the UI
  // copy, not here.
  const handleToggleQueryLogging = async (checked: boolean) => {
    setIsSavingQueryLogging(true)
    try {
      await dnsServerService.updateSettings({
        interfaces: selectedInterfaces,
        queryLogging: checked,
        dnsCacheTtlMinutes: Number(dnsCacheTtlInput) || DNS_CACHE_TTL_DEFAULT,
        dnsCacheMaxEntries: Number(dnsCacheMaxInput) || DNS_CACHE_ENTRIES_DEFAULT,
        upstreamMode,
        upstreamServers,
      })
      setQueryLogging(checked)
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถบันทึกการเปิด/ปิดสถิติ DNS ได้: " + getErrorMessage(err))
    } finally {
      setIsSavingQueryLogging(false)
    }
  }

  // Saving the TTL/cap never restarts dnsmasq server-side (SetReverseCacheLimits
  // only, not ApplyAll — see api/handlers.go HandleUpdateDNSServerSettings) so
  // isApplied/"Apply DNS Zones" banner is intentionally left untouched here.
  const handleSaveCacheLimits = async () => {
    const ttl = Number(dnsCacheTtlInput)
    const max = Number(dnsCacheMaxInput)
    if (!Number.isFinite(ttl) || ttl < DNS_CACHE_TTL_MIN || ttl > DNS_CACHE_TTL_MAX) {
      setDnsCacheLimitsError(`อายุของ mapping ต้องอยู่ระหว่าง ${DNS_CACHE_TTL_MIN}-${DNS_CACHE_TTL_MAX} นาที`)
      return
    }
    if (!Number.isFinite(max) || max < DNS_CACHE_ENTRIES_MIN || max > DNS_CACHE_ENTRIES_MAX) {
      setDnsCacheLimitsError(`จำนวน mapping สูงสุดต้องอยู่ระหว่าง ${DNS_CACHE_ENTRIES_MIN}-${DNS_CACHE_ENTRIES_MAX}`)
      return
    }
    setDnsCacheLimitsError("")
    setIsSavingCacheLimits(true)
    try {
      await dnsServerService.updateSettings({
        interfaces: selectedInterfaces,
        queryLogging,
        dnsCacheTtlMinutes: ttl,
        dnsCacheMaxEntries: max,
        upstreamMode,
        upstreamServers,
      })
    } catch (err) {
      // The backend re-validates (400 on out-of-range) even though the
      // client already checked above — surface that error verbatim.
      setDnsCacheLimitsError(getErrorMessage(err))
    } finally {
      setIsSavingCacheLimits(false)
    }
  }

  // --- Handlers: Upstream Resolvers (docs/ref/todo/
  // dns-server-settings-tab-and-upstream-plan.md T-12) ---
  // Effective "system" upstreams, computed the same way the backend does in
  // resolveUpstreams (static -> primary/secondary; wan -> DHCP-provided DNS
  // per WAN link) — shown read-only so the user knows what will actually be
  // used without duplicating that value into this page's own state.
  const effectiveSystemUpstreams = useMemo(() => {
    if (!systemDnsConfig) return []
    if (systemDnsConfig.mode === "static") {
      return [systemDnsConfig.primaryDns, systemDnsConfig.secondaryDns].filter(Boolean)
    }
    return (systemDnsConfig.dynamicDnsServers || []).flatMap(d => d.dnsServers)
  }, [systemDnsConfig])

  const handleUpstreamModeChange = (mode: "system" | "custom") => {
    setUpstreamMode(mode)
    setUpstreamError("")
  }

  const handleUpstreamServerChange = (index: number, value: string) => {
    setUpstreamServers(prev => prev.map((v, i) => i === index ? value : v))
  }

  const handleAddUpstreamServer = () => {
    if (upstreamServers.length >= DNS_UPSTREAM_MAX_SERVERS) return
    setUpstreamServers(prev => [...prev, ""])
  }

  const handleRemoveUpstreamServer = (index: number) => {
    setUpstreamServers(prev => prev.filter((_, i) => i !== index))
  }

  const handleSaveUpstream = async () => {
    setUpstreamError("")

    const trimmed = upstreamServers.map(s => s.trim())
    if (upstreamMode === "custom") {
      if (trimmed.length === 0) {
        setUpstreamError("โปรดกรอก Upstream DNS Server อย่างน้อย 1 รายการ")
        return
      }
      for (const ip of trimmed) {
        if (!isValidIp(ip)) {
          setUpstreamError(`"${ip}" ไม่ใช่ IP address ที่ถูกต้อง (รับเฉพาะ IP เปล่า ไม่รองรับ port/ชื่อโฮสต์/DoH/DoT)`)
          return
        }
      }
      const dedup = new Set(trimmed)
      if (dedup.size !== trimmed.length) {
        setUpstreamError("มี IP ซ้ำกันในรายการ")
        return
      }
    }

    setIsSavingUpstream(true)
    try {
      await dnsServerService.updateSettings({
        interfaces: selectedInterfaces,
        queryLogging,
        dnsCacheTtlMinutes: Number(dnsCacheTtlInput) || DNS_CACHE_TTL_DEFAULT,
        dnsCacheMaxEntries: Number(dnsCacheMaxInput) || DNS_CACHE_ENTRIES_DEFAULT,
        upstreamMode,
        upstreamServers: upstreamMode === "custom" ? trimmed : upstreamServers,
      })
      if (upstreamMode === "custom") {
        setUpstreamServers(trimmed)
      }
      setIsApplied(false)
    } catch (err) {
      // Surface the backend's 400 verbatim (defense-in-depth revalidation).
      setUpstreamError(getErrorMessage(err))
    } finally {
      setIsSavingUpstream(false)
    }
  }

  // Dangling ("Missing") interface names: saved in settings but no longer present in
  // the LAN interface list (deleted VLAN, parent gone, or role changed off LAN). The
  // backend grandfathers these so they can be un-ticked to remove; they can't be
  // re-added since they're absent from availableInterfaces (issue #46).
  const missingInterfaces = useMemo(() => {
    return selectedInterfaces.filter(n => !availableInterfaces.some(i => i.name === n))
  }, [selectedInterfaces, availableInterfaces])

  // Selected Zone object
  const selectedZone = useMemo(() => {
    return zones.find(z => z.id === selectedZoneId) || null
  }, [zones, selectedZoneId])

  // Filtered Zones
  const filteredZones = useMemo(() => {
    return zones.filter(z => 
      z.zoneName.toLowerCase().includes(zoneSearchQuery.toLowerCase())
    )
  }, [zones, zoneSearchQuery])

  // Filtered Records
  const filteredRecords = useMemo(() => {
    if (!selectedZone) return []
    if (!selectedZone.records || selectedZone.records.length === 0) return []
    return selectedZone.records.filter(r => 
      r.name.toLowerCase().includes(recordSearchQuery.toLowerCase()) ||
      r.type.toLowerCase().includes(recordSearchQuery.toLowerCase()) ||
      r.value.toLowerCase().includes(recordSearchQuery.toLowerCase())
    )
  }, [selectedZone, recordSearchQuery])

  // Filtered Blocked Domains
  const filteredBlockedDomains = useMemo(() => {
    const q = blockedSearchQuery.toLowerCase()
    return blockedDomains.filter(b =>
      b.domain.toLowerCase().includes(q) || (b.comment || "").toLowerCase().includes(q)
    )
  }, [blockedDomains, blockedSearchQuery])

  // Blocklists summary bar totals (docs/ref/todo/dns-blocklist-import-plan.md
  // T-12 item 2): overall domain count across every list/mode, plus the
  // narrower nxdomain-only total (which has a separate, lower cap since it
  // costs a re-parse of its conf-file on every Apply — plan §2.1.4).
  const blocklistTotalDomains = useMemo(
    () => blocklists.reduce((sum, b) => sum + b.domainCount, 0),
    [blocklists]
  )
  const blocklistNXDomainDomains = useMemo(
    () => blocklists.filter(b => b.blockMode === "nxdomain").reduce((sum, b) => sum + b.domainCount, 0),
    [blocklists]
  )

  const formatBlocklistTime = (iso?: string): string => {
    if (!iso) return "—"
    const d = new Date(iso)
    if (isNaN(d.getTime())) return "—"
    return d.toLocaleString()
  }

  const blocklistUrlHost = (url?: string): string => {
    if (!url) return ""
    try {
      return new URL(url).hostname
    } catch {
      return url
    }
  }

  // Read-only suggestion (T-12, plan §5 item 3/§2 "Migration path ของ
  // empty-zone workaround"): zones that look like they were created purely
  // as the old empty-zone-NXDOMAIN workaround. No mutation is offered here —
  // deleting/moving them is left entirely to the user.
  const emptyZoneSuggestions = useMemo(() => {
    return zones.filter(z => z.isAuthoritative && z.enabled && (z.records || []).length === 0)
  }, [zones])

  // --- Handlers: Zone CRUD ---
  const openCreateZoneModal = () => {
    setEditingZone(null)
    setZoneName("")
    setIsAuthoritative(true)
    setForwardTo("")
    setAllowedIps("any")
    setZoneError("")
    setIsZoneModalOpen(true)
  }

  const openEditZoneModal = (zone: DNSZone) => {
    setEditingZone(zone)
    setZoneName(zone.zoneName)
    setIsAuthoritative(zone.isAuthoritative)
    setForwardTo(zone.forwardTo || "")
    setAllowedIps(zone.allowedIps || "any")
    setZoneError("")
    setIsZoneModalOpen(true)
  }

  const handleDeleteZone = async (id: string, name: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบโซน DNS "${name}" ใช่หรือไม่? (ระเบียนในโซนทั้งหมดจะถูกลบไปด้วย)`)) {
      try {
        await dnsServerService.deleteZone(id)
        setZones(prev => prev.filter(z => z.id !== id))
        if (selectedZoneId === id) {
          const remaining = zones.filter(z => z.id !== id)
          setSelectedZoneId(remaining.length > 0 ? remaining[0].id : null)
        }
        setIsApplied(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบโซนได้: " + getErrorMessage(err))
      }
    }
  }

  const handleToggleZone = async (id: string, checked: boolean) => {
    try {
      await dnsServerService.toggleZone(id)
      setZones(prev => prev.map(z => z.id === id ? { ...z, enabled: checked } : z))
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปิด/ปิดโซนได้: " + getErrorMessage(err))
    }
  }

  const handleSaveZone = async (e: React.FormEvent) => {
    e.preventDefault()
    setZoneError("")
    setIsSaving(true)

    const name = zoneName.trim()
    const forward = forwardTo.trim()

    if (!name) {
      setZoneError("กรุณากรอกชื่อโซน")
      setIsSaving(false)
      return
    }

    if (!isAuthoritative) {
      if (!forward) {
        setZoneError("โซนประเภท Forward (ส่งต่อ) จำเป็นต้องระบุ IP ของ Upstream Resolver")
        setIsSaving(false)
        return
      }
      if (!isValidIp(forward)) {
        setZoneError("IP สำหรับส่งต่อ (Forward To) ไม่ถูกต้อง")
        setIsSaving(false)
        return
      }
    }

    try {
      const payload = {
        zoneName: name,
        forwardTo: isAuthoritative ? "" : forward,
        allowedIps: allowedIps,
        isAuthoritative: isAuthoritative,
        enabled: editingZone ? editingZone.enabled : true
      }

      if (editingZone) {
        const updated = await dnsServerService.updateZone(editingZone.id, payload)
        // Zone update only touches zone metadata — always keep the records already held
        // locally rather than trusting whatever (possibly empty) records the API echoes back.
        setZones(prev => prev.map(z => z.id === editingZone.id ? { ...z, ...updated, records: z.records } : z))
      } else {
        const created = await dnsServerService.createZone(payload)
        setZones(prev => [...prev, created])
        setSelectedZoneId(created.id)
      }

      setIsZoneModalOpen(false)
      setIsApplied(false)
    } catch (err) {
      setZoneError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    } finally {
      setIsSaving(false)
    }
  }

  // --- Handlers: Record CRUD ---
  const openCreateRecModal = () => {
    setEditingRecord(null)
    setRecName("")
    setRecType("A")
    setRecValue("")
    setRecTtl("300")
    setRecError("")
    setIsRecModalOpen(true)
  }

  const openEditRecModal = (rec: DNSRecord) => {
    setEditingRecord(rec)
    setRecName(rec.name)
    setRecType(rec.type)
    setRecValue(rec.value)
    setRecTtl(rec.ttl.toString())
    setRecError("")
    setIsRecModalOpen(true)
  }

  const handleDeleteRecord = async (id: string, name: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบระเบียน DNS "${name}" ใช่หรือไม่?`)) {
      try {
        await dnsServerService.deleteRecord(id)
        setZones(prev => prev.map(z => {
          if (z.id === selectedZoneId) {
            return {
              ...z,
              records: z.records.filter(r => r.id !== id)
            }
          }
          return z
        }))
        setIsApplied(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบระเบียนได้: " + getErrorMessage(err))
      }
    }
  }

  const handleSaveRecord = async (e: React.FormEvent) => {
    e.preventDefault()
    setRecError("")
    setIsSaving(true)

    const name = recName.trim()
    const value = recValue.trim()
    const ttlVal = parseInt(recTtl, 10)

    if (!value) {
      setRecError("กรุณากรอกค่าระเบียน (Record Value)")
      setIsSaving(false)
      return
    }

    if (isNaN(ttlVal) || ttlVal <= 0) {
      setRecError("TTL ต้องเป็นตัวเลขจำนวนเต็มบวก")
      setIsSaving(false)
      return
    }

    // Type validation
    if (recType === "A" && !isValidIp(value)) {
      setRecError("สำหรับระเบียนประเภท A ค่าของระเบียนต้องเป็น IPv4 แอดเดรสที่ถูกต้อง")
      setIsSaving(false)
      return
    }

    if (recType === "NS") {
      // Client-side UX check only — mirrors backend model.EncodeDNSNameHex
      // (trailing dot trimmed; labels are [A-Za-z0-9-], 1-63 chars, no
      // leading/trailing '-', no empty label; total length <=253). The
      // backend remains the actual gatekeeper.
      const target = value.replace(/\.$/, "")
      const labelRe = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/
      const labels = target.split(".")
      const isValidNsName =
        target.length > 0 &&
        target.length <= 253 &&
        labels.every((label) => labelRe.test(label))
      if (!isValidNsName) {
        setRecError("สำหรับระเบียนประเภท NS ค่าของระเบียนต้องเป็นชื่อ nameserver ที่ถูกต้อง เช่น ns1.example.com")
        setIsSaving(false)
        return
      }
    }

    try {
      const payload = {
        name: name || "@",
        type: recType,
        value: value,
        ttl: ttlVal
      }

      if (editingRecord) {
        const updated = await dnsServerService.updateRecord(editingRecord.id, payload)
        setZones(prev => prev.map(z => {
          if (z.id === selectedZoneId) {
            return {
              ...z,
              records: z.records.map(r => r.id === editingRecord.id ? updated : r)
            }
          }
          return z
        }))
      } else {
        const created = await dnsServerService.createRecord(selectedZoneId!, payload)
        setZones(prev => prev.map(z => {
          if (z.id === selectedZoneId) {
            return {
              ...z,
              records: [...z.records, created]
            }
          }
          return z
        }))
      }

      setIsRecModalOpen(false)
      setIsApplied(false)
    } catch (err) {
      setRecError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูลระเบียน")
    } finally {
      setIsSaving(false)
    }
  }

  // --- Handlers: Blocked Domains CRUD (deny-list) ---
  const openCreateBlockedModal = () => {
    setEditingBlockedDomain(null)
    setBlkDomain("")
    setBlkMode("nxdomain")
    setBlkComment("")
    setBlkEnabled(true)
    setBlkError("")
    setIsBlockedModalOpen(true)
  }

  const openEditBlockedModal = (b: BlockedDomain) => {
    setEditingBlockedDomain(b)
    setBlkDomain(b.domain)
    setBlkMode(b.mode)
    setBlkComment(b.comment || "")
    setBlkEnabled(b.enabled)
    setBlkError("")
    setIsBlockedModalOpen(true)
  }

  const handleDeleteBlockedDomain = async (id: string, domain: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบโดเมนที่ถูกบล็อก "${domain}" ใช่หรือไม่?`)) {
      try {
        await dnsServerService.deleteBlockedDomain(id)
        setBlockedDomains(prev => prev.filter(b => b.id !== id))
        setIsApplied(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบโดเมนที่ถูกบล็อกได้: " + getErrorMessage(err))
      }
    }
  }

  const handleToggleBlockedDomain = async (id: string, checked: boolean) => {
    try {
      await dnsServerService.toggleBlockedDomain(id)
      setBlockedDomains(prev => prev.map(b => b.id === id ? { ...b, enabled: checked } : b))
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปิด/ปิดโดเมนที่ถูกบล็อกได้: " + getErrorMessage(err))
    }
  }

  const handleSaveBlockedDomain = async (e: React.FormEvent) => {
    e.preventDefault()
    setBlkError("")

    const domain = blkDomain.trim().toLowerCase()
    if (!domain) {
      setBlkError("กรุณากรอกชื่อโดเมน")
      return
    }
    if (!editingBlockedDomain && blockedDomains.length >= DNS_BLOCKED_DOMAINS_MAX) {
      setBlkError(`รายการโดเมนที่ถูกบล็อกครบจำนวนสูงสุด ${DNS_BLOCKED_DOMAINS_MAX} รายการแล้ว`)
      return
    }

    setIsSavingBlocked(true)
    try {
      const payload = { domain, mode: blkMode, enabled: blkEnabled, comment: blkComment.trim() }
      if (editingBlockedDomain) {
        const updated = await dnsServerService.updateBlockedDomain(editingBlockedDomain.id, payload)
        setBlockedDomains(prev => prev.map(b => b.id === editingBlockedDomain.id ? updated : b))
      } else {
        const created = await dnsServerService.createBlockedDomain(payload)
        setBlockedDomains(prev => [...prev, created])
      }
      setIsBlockedModalOpen(false)
      setIsApplied(false)
    } catch (err) {
      setBlkError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    } finally {
      setIsSavingBlocked(false)
    }
  }

  // --- Handlers: DNS Blocklists (bulk import, docs/ref/todo/
  // dns-blocklist-import-plan.md) ---
  const closeBlocklistModal = () => {
    setIsBlocklistModalOpen(false)
    // Reset back to the defaults every time the form closes (create, edit,
    // Cancel, or Escape/overlay dismiss) — instruction T-12 item 3b.
    setBlstSourceType("url")
    setBlstMode("sinkhole")
  }

  const openCreateBlocklistModal = () => {
    setEditingBlocklist(null)
    setBlstSourceType("url")
    setBlstName("")
    setBlstUrl("")
    setBlstMode("sinkhole")
    setBlstEnabled(true)
    setBlstFile(null)
    setBlstError("")
    setIsBlocklistModalOpen(true)
  }

  const openEditBlocklistModal = (b: DNSBlocklist) => {
    setEditingBlocklist(b)
    setBlstSourceType(b.sourceType)
    setBlstName(b.name)
    setBlstUrl(b.url || "")
    setBlstMode(b.blockMode)
    setBlstEnabled(b.enabled)
    setBlstFile(null)
    setBlstError("")
    setIsBlocklistModalOpen(true)
  }

  const handleDeleteBlocklist = async (id: string, name: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบ Blocklist "${name}" ใช่หรือไม่? (ไฟล์โดเมนที่ import ไว้จะถูกลบไปด้วย)`)) {
      setBlocklistBusyIds(prev => new Set(prev).add(id))
      try {
        await dnsServerService.deleteBlocklist(id)
        setBlocklists(prev => prev.filter(b => b.id !== id))
        setIsApplied(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบ Blocklist ได้: " + getErrorMessage(err))
      } finally {
        setBlocklistBusyIds(prev => {
          const next = new Set(prev)
          next.delete(id)
          return next
        })
      }
    }
  }

  const handleToggleBlocklist = async (id: string) => {
    setBlocklistBusyIds(prev => new Set(prev).add(id))
    try {
      const updated = await dnsServerService.toggleBlocklist(id)
      setBlocklists(prev => prev.map(b => b.id === id ? updated : b))
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปิด/ปิด Blocklist ได้: " + getErrorMessage(err))
    } finally {
      setBlocklistBusyIds(prev => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    }
  }

  const handleRefreshBlocklist = async (id: string) => {
    setBlocklistBusyIds(prev => new Set(prev).add(id))
    try {
      const updated = await dnsServerService.refreshBlocklist(id)
      setBlocklists(prev => prev.map(b => b.id === id ? updated : b))
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถรีเฟรช Blocklist ได้: " + getErrorMessage(err))
    } finally {
      setBlocklistBusyIds(prev => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    }
  }

  const handleSaveBlocklist = async (e: React.FormEvent) => {
    e.preventDefault()
    setBlstError("")

    const name = blstName.trim()
    if (!name) {
      setBlstError("กรุณากรอกชื่อ Blocklist")
      return
    }
    if (!editingBlocklist && blocklists.length >= DNS_BLOCKLISTS_MAX) {
      setBlstError(`รายการ Blocklist ครบจำนวนสูงสุด ${DNS_BLOCKLISTS_MAX} รายการแล้ว`)
      return
    }

    if (editingBlocklist) {
      // Edit only touches name/url/blockMode/enabled — the source (url vs
      // upload) and its data can't be changed here; delete + re-add instead.
      setIsSavingBlocklist(true)
      try {
        const updated = await dnsServerService.updateBlocklist(editingBlocklist.id, {
          name,
          url: editingBlocklist.sourceType === "url" ? blstUrl.trim() : undefined,
          blockMode: blstMode,
          enabled: blstEnabled,
        })
        setBlocklists(prev => prev.map(b => b.id === editingBlocklist.id ? updated : b))
        closeBlocklistModal()
        setIsApplied(false)
      } catch (err) {
        setBlstError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
      } finally {
        setIsSavingBlocklist(false)
      }
      return
    }

    if (blstSourceType === "url") {
      const url = blstUrl.trim()
      if (!url) {
        setBlstError("กรุณากรอก URL ของ Blocklist")
        return
      }
      if (!/^https:\/\//i.test(url)) {
        setBlstError("URL ต้องขึ้นต้นด้วย https:// เท่านั้น (มาตรการความปลอดภัย — ไม่รองรับ http://)")
        return
      }
      setIsSavingBlocklist(true)
      try {
        const created = await dnsServerService.createBlocklistFromUrl(name, url, blstMode, blstEnabled)
        setBlocklists(prev => [...prev, created])
        closeBlocklistModal()
        setIsApplied(false)
      } catch (err) {
        setBlstError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการดึงข้อมูล Blocklist จาก URL")
      } finally {
        setIsSavingBlocklist(false)
      }
    } else {
      if (!blstFile) {
        setBlstError("กรุณาเลือกไฟล์ hosts ที่ต้องการอัปโหลด")
        return
      }
      if (blstFile.size > DNS_BLOCKLIST_MAX_FILE_MB * 1024 * 1024) {
        setBlstError(`ไฟล์ใหญ่เกิน ${DNS_BLOCKLIST_MAX_FILE_MB} MB (ไฟล์ที่เลือกมีขนาด ${(blstFile.size / (1024 * 1024)).toFixed(1)} MB)`)
        return
      }
      setIsSavingBlocklist(true)
      try {
        const created = await dnsServerService.uploadBlocklist(name, blstFile, blstMode, blstEnabled)
        setBlocklists(prev => [...prev, created])
        closeBlocklistModal()
        setIsApplied(false)
      } catch (err) {
        setBlstError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการอัปโหลดไฟล์ Blocklist")
      } finally {
        setIsSavingBlocklist(false)
      }
    }
  }

  // --- Handlers: Apply & Cache ---
  const handleApplySettings = async () => {
    setIsApplying(true)
    try {
      await dnsServerService.apply()
      setIsApplied(true)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเริ่มระบบ DNS เข้ากับ OS Kernel ได้: " + getErrorMessage(err))
    } finally {
      setIsApplying(false)
    }
  }

  const handleClearCache = async () => {
    setIsClearingCache(true)
    try {
      await dnsServerService.clearCache()
      await alert("สำเร็จ", "เคลียร์หน่วยความจำแคช DNS ของระบบเรียบร้อยแล้ว")
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเคลียร์หน่วยความจำแคชได้: " + getErrorMessage(err))
    } finally {
      setIsClearingCache(false)
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="ml-2 text-sm text-muted-foreground">กำลังโหลดข้อมูล DNS Server...</span>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <CapabilityBanner id="dnsmasq" />

      {/* Header bar: Clear Cache / Apply DNS Zones — moved above the tabs so
          both are reachable regardless of which tab is active (T-12 item 1). */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-foreground">DNS Server</h2>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            onClick={handleClearCache}
            disabled={isClearingCache}
            variant="outline"
            size="sm"
            className="cursor-pointer gap-2"
          >
            <RefreshCw className={`h-4 w-4 ${isClearingCache ? "animate-spin" : ""}`} />
            Clear Cache
          </Button>
          {!isApplied && (
            <Button
              size="sm"
              onClick={handleApplySettings}
              disabled={isApplying}
              className="animate-pulse cursor-pointer gap-1.5 bg-warning font-semibold text-warning-foreground hover:bg-warning/90"
            >
              {isApplying ? (
                <>
                  <RefreshCw className="h-4 w-4 animate-spin" />
                  Applying...
                </>
              ) : (
                <>
                  <Check className="h-4 w-4" />
                  Apply DNS Zones
                </>
              )}
            </Button>
          )}
          {isApplied && (
            <div className="flex h-8 items-center gap-1.5 rounded-lg border border-primary/20 bg-primary/10 px-3 text-xs font-medium text-primary">
              <CheckCircle2 className="h-4 w-4" />
              DNS Server Synced
            </div>
          )}
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="zones" className="cursor-pointer">Zones &amp; Records</TabsTrigger>
          <TabsTrigger value="blocked" className="cursor-pointer">Blocked Domains</TabsTrigger>
          <TabsTrigger value="blocklists" className="cursor-pointer">Blocklists</TabsTrigger>
          <TabsTrigger value="settings" className="cursor-pointer">Settings</TabsTrigger>
        </TabsList>

        <TabsContent value="zones" className="space-y-4">
          {/* Split Screen Zones / Records */}
          <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-12">
            {/* Left Side: Zones list */}
            <Card className="lg:col-span-4">
              <CardHeader className="flex flex-row items-center justify-between space-y-0">
                <CardTitle className="flex items-center gap-2 text-base font-semibold">
                  <Database className="h-4 w-4 text-muted-foreground" />
                  DNS Zones
                  <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                    {zones.length}
                  </Badge>
                </CardTitle>
                <Button
                  onClick={openCreateZoneModal}
                  size="sm"
                  className="cursor-pointer gap-1.5 font-semibold"
                >
                  <Plus className="h-4 w-4" />
                  Add Zone
                </Button>
              </CardHeader>

              <CardContent className="space-y-3">
                <div className="relative">
                  <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
                  <Input
                    type="text"
                    value={zoneSearchQuery}
                    onChange={(e) => setZoneSearchQuery(e.target.value)}
                    placeholder="ค้นหาโดเมน/โซน..."
                    className="h-9 pl-8 text-xs"
                  />
                </div>

                <div className="max-h-[480px] space-y-2 overflow-y-auto pr-1">
                  {filteredZones.map(zone => (
                    <div
                      key={zone.id}
                      onClick={() => setSelectedZoneId(zone.id)}
                      className={`flex cursor-pointer items-center justify-between rounded-lg border p-3 transition ${
                        selectedZoneId === zone.id
                          ? "border-primary/40 bg-primary/10 text-foreground"
                          : "border-border bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground"
                      }`}
                    >
                      <div className="flex-1 select-none space-y-1">
                        <div className="flex items-center gap-1.5">
                          <span className="font-mono text-xs font-semibold">{zone.zoneName}</span>
                          <Badge
                            variant="outline"
                            className={`rounded px-1.5 py-0 text-[10px] font-medium ${zone.isAuthoritative
                              ? "border-primary/20 bg-primary/10 text-primary"
                              : "border-border bg-muted text-muted-foreground"
                              }`}
                          >
                            {zone.isAuthoritative ? "Local" : "Fwd"}
                          </Badge>
                        </div>
                        {!zone.isAuthoritative && zone.forwardTo && (
                          <p className="font-mono text-[10px] text-muted-foreground/60">
                            {"->"} {zone.forwardTo}
                          </p>
                        )}
                      </div>

                      <div className="ml-2 flex items-center gap-2">
                        <Switch
                          size="sm"
                          checked={zone.enabled}
                          onCheckedChange={(checked) => handleToggleZone(zone.id, checked)}
                          className="cursor-pointer"
                        />
                        <div className="flex items-center gap-1">
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={(e) => {
                              e.stopPropagation()
                              openEditZoneModal(zone)
                            }}
                            className="cursor-pointer text-muted-foreground hover:bg-muted hover:text-foreground"
                            title="แก้ไขโซน"
                          >
                            <Edit className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={(e) => {
                              e.stopPropagation()
                              handleDeleteZone(zone.id, zone.zoneName)
                            }}
                            className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                            title="ลบโซน"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))}

                  {filteredZones.length === 0 && (
                    <div className="p-6 text-center text-xs text-muted-foreground">
                      {zoneSearchQuery ? "ไม่พบโซนที่ค้นหา" : "ยังไม่มีการสร้าง DNS Zone"}
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>

            {/* Right Side: DNS Records list for selected zone */}
            <div className="space-y-4 lg:col-span-8">
              {selectedZone ? (
                <Card>
                  <CardHeader className="flex flex-col gap-3 space-y-0 sm:flex-row sm:items-center sm:justify-between">
                    <div className="space-y-1">
                      <CardTitle className="flex items-center gap-2 text-base font-semibold">
                        <Server className="h-4 w-4 text-muted-foreground" />
                        DNS Records ของโซน <span className="font-mono">{selectedZone.zoneName}</span>
                      </CardTitle>
                      <CardDescription className="text-xs">
                        {selectedZone.isAuthoritative
                          ? "Local zone (ตอบจาก records ในระบบ ชื่อที่ไม่รู้จักตอบ NXDOMAIN)"
                          : `โซน Forward (ระบบจะทำการส่งต่อคิวรีทั้งหมดไปให้ ${selectedZone.forwardTo})`}
                      </CardDescription>
                    </div>
                    {selectedZone.isAuthoritative && (
                      <Button
                        onClick={openCreateRecModal}
                        size="sm"
                        className="cursor-pointer gap-1.5 font-semibold"
                      >
                        <Plus className="h-4 w-4" />
                        Add Record
                      </Button>
                    )}
                  </CardHeader>

                  <CardContent className="space-y-4">
                    {selectedZone.isAuthoritative ? (
                      <>
                        <div className="relative">
                          <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
                          <Input
                            type="text"
                            value={recordSearchQuery}
                            onChange={(e) => setRecordSearchQuery(e.target.value)}
                            placeholder="ค้นหาชื่อระเบียน, ประเภท หรือข้อมูล..."
                            className="h-9 pl-8 text-xs"
                          />
                        </div>

                        <Table>
                          <TableHeader>
                            <TableRow className="hover:bg-transparent">
                              <TableHead className="w-[25%] text-xs font-medium text-muted-foreground">Host Name</TableHead>
                              <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Type</TableHead>
                              <TableHead className="w-[40%] text-xs font-medium text-muted-foreground">Value</TableHead>
                              <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">TTL</TableHead>
                              <TableHead className="w-[10%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {filteredRecords.length === 0 ? (
                              <TableRow>
                                <TableCell colSpan={5} className="py-8 text-center text-xs text-muted-foreground">
                                  {recordSearchQuery ? "ไม่พบระเบียน DNS ตามข้อความค้นหา" : "ยังไม่มีข้อมูลระเบียน DNS ในโซนนี้"}
                                </TableCell>
                              </TableRow>
                            ) : (
                              filteredRecords.map((rec) => (
                                <TableRow key={rec.id}>
                                  <TableCell className="py-3">
                                    <span className="text-xs font-medium text-foreground">{rec.name}</span>
                                  </TableCell>
                                  <TableCell className="py-3">
                                    <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-medium text-primary">
                                      {rec.type}
                                    </Badge>
                                  </TableCell>
                                  <TableCell className="max-w-[200px] truncate py-3 font-mono text-xs font-medium text-foreground" title={rec.value}>
                                    {rec.value}
                                  </TableCell>
                                  <TableCell className="py-3 font-mono text-xs text-muted-foreground">
                                    {rec.ttl}s
                                  </TableCell>
                                  <TableCell className="py-3 text-right">
                                    <div className="flex items-center justify-end gap-2">
                                      <Button
                                        variant="outline"
                                        size="icon-sm"
                                        onClick={() => openEditRecModal(rec)}
                                        className="cursor-pointer text-muted-foreground hover:text-foreground"
                                        title="แก้ไขระเบียน"
                                      >
                                        <Edit className="h-4 w-4" />
                                      </Button>
                                      <Button
                                        variant="ghost"
                                        size="icon-sm"
                                        onClick={() => handleDeleteRecord(rec.id, rec.name)}
                                        className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                        title="ลบระเบียน"
                                      >
                                        <Trash2 className="h-4 w-4" />
                                      </Button>
                                    </div>
                                  </TableCell>
                                </TableRow>
                              ))
                            )}
                          </TableBody>
                        </Table>
                      </>
                    ) : (
                      <div className="space-y-2 rounded-lg border border-dashed border-border p-8 text-center">
                        <Globe className="mx-auto h-8 w-8 text-muted-foreground/60" />
                        <p className="text-sm font-semibold text-foreground">โซนนี้ได้รับการตั้งค่าประเภทส่งต่อ (Forward Zone)</p>
                        <p className="mx-auto max-w-md text-xs leading-relaxed text-muted-foreground">
                          ระบบจะส่งคำขอความละเอียดชื่อระบบโดเมนทั้งหมดภายใต้โดเมน <strong className="text-foreground">{selectedZone.zoneName}</strong> ไปยัง
                          ที่อยู่ IP <strong className="font-mono text-foreground">{selectedZone.forwardTo}</strong> โดยอัตโนมัติ
                          คุณไม่จำเป็นต้องเพิ่มระเบียน DNS เอง
                        </p>
                      </div>
                    )}
                  </CardContent>
                </Card>
              ) : (
                <Card>
                  <CardContent className="flex flex-col items-center justify-center gap-2 py-8 text-center">
                    <Globe className="h-8 w-8 text-muted-foreground/50" />
                    <p className="text-sm text-muted-foreground">กรุณาเลือกโซนด้านซ้าย หรือกดสร้างโซนใหม่เพื่อจัดการระเบียน DNS</p>
                  </CardContent>
                </Card>
              )}
            </div>
          </div>

          {/* Info note */}
          <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
            <Info className="mt-0.5 h-4 w-4 shrink-0" />
            <div>
              <strong className="text-foreground">ระเบียนประเภทต่างๆ ของ DNS Server:</strong>
              <ul className="mt-1 list-disc space-y-1 pl-4">
                <li><strong className="text-foreground">A</strong>: ชี้โดเมนย่อยไปที่ที่อยู่ IPv4 (เช่น router.pigate.local {"->"} 192.168.1.1)</li>
                <li><strong className="text-foreground">AAAA</strong>: ชี้โดเมนย่อยไปที่ที่อยู่ IPv6</li>
                <li><strong className="text-foreground">CNAME</strong>: ชื่อสมญา/ส่งต่อไปหาชื่อเครื่องอื่น (เช่น printer.pigate.local {"->"} hp-laser.pigate.local)</li>
                <li><strong className="text-foreground">MX</strong>: ชี้เซิร์ฟเวอร์รับส่งอีเมลประจำโดเมน (ระบุรูปแบบ [Preference] [Host] เช่น 10 mail.example.com)</li>
                <li><strong className="text-foreground">TXT</strong>: ระบุข้อมูลข้อความทั่วไป เช่น SPF หรือคีย์ยืนยันตัวตน</li>
                <li><strong className="text-foreground">NS</strong>: ระบุ nameserver ของโดเมน/โดเมนย่อย (PiGate จะประกาศ (publish) ระเบียน NS นี้ให้เท่านั้น ไม่ได้ส่งต่อ (delegate) คำถามใต้โดเมนนั้นไปยัง nameserver ที่ระบุจริง)</li>
              </ul>
            </div>
          </div>
        </TabsContent>

        <TabsContent value="blocked" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Ban className="h-4 w-4 text-muted-foreground" />
                Blocked Domains
                <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                  {blockedDomains.length}/{DNS_BLOCKED_DOMAINS_MAX}
                </Badge>
              </CardTitle>
              <Button
                onClick={openCreateBlockedModal}
                size="sm"
                disabled={blockedDomains.length >= DNS_BLOCKED_DOMAINS_MAX}
                className="cursor-pointer gap-1.5 font-semibold"
              >
                <Plus className="h-4 w-4" />
                Add Domain
              </Button>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="relative">
                <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  type="text"
                  value={blockedSearchQuery}
                  onChange={(e) => setBlockedSearchQuery(e.target.value)}
                  placeholder="ค้นหาโดเมนที่ถูกบล็อก..."
                  className="h-9 pl-8 text-xs"
                />
              </div>

              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="w-[35%] text-xs font-medium text-muted-foreground">Domain</TableHead>
                    <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Mode</TableHead>
                    <TableHead className="w-[30%] text-xs font-medium text-muted-foreground">Comment</TableHead>
                    <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Enabled</TableHead>
                    <TableHead className="w-[10%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filteredBlockedDomains.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5} className="py-8 text-center text-xs text-muted-foreground">
                        {blockedSearchQuery ? "ไม่พบโดเมนที่ค้นหา" : "ยังไม่มีโดเมนที่ถูกบล็อก"}
                      </TableCell>
                    </TableRow>
                  ) : (
                    filteredBlockedDomains.map((b) => (
                      <TableRow key={b.id}>
                        <TableCell className="py-3">
                          <span className="font-mono text-xs font-medium text-foreground">{b.domain}</span>
                        </TableCell>
                        <TableCell className="py-3">
                          <Badge
                            variant="outline"
                            className={`rounded px-1.5 py-0 text-[10px] font-medium ${b.mode === "sinkhole"
                              ? "border-warning/30 bg-warning/10 text-warning"
                              : "border-primary/20 bg-primary/10 text-primary"
                              }`}
                          >
                            {b.mode === "sinkhole" ? "Sinkhole" : "NXDOMAIN"}
                          </Badge>
                        </TableCell>
                        <TableCell className="max-w-[220px] truncate py-3 text-xs text-muted-foreground" title={b.comment}>
                          {b.comment || "-"}
                        </TableCell>
                        <TableCell className="py-3">
                          <Switch
                            size="sm"
                            checked={b.enabled}
                            onCheckedChange={(checked) => handleToggleBlockedDomain(b.id, checked)}
                            className="cursor-pointer"
                          />
                        </TableCell>
                        <TableCell className="py-3 text-right">
                          <div className="flex items-center justify-end gap-2">
                            <Button
                              variant="outline"
                              size="icon-sm"
                              onClick={() => openEditBlockedModal(b)}
                              className="cursor-pointer text-muted-foreground hover:text-foreground"
                              title="แก้ไข"
                            >
                              <Edit className="h-4 w-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => handleDeleteBlockedDomain(b.id, b.domain)}
                              className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                              title="ลบ"
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          {/* Info note */}
          <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
            <Info className="mt-0.5 h-4 w-4 shrink-0" />
            <div className="space-y-1">
              <p><strong className="text-foreground">การบล็อกครอบคลุมโดเมนย่อยทั้งหมดโดยอัตโนมัติ</strong> — บล็อก <span className="font-mono">ads.example.com</span> จะบล็อก <span className="font-mono">www.ads.example.com</span> ด้วย ไม่มีโหมดบล็อกเฉพาะชื่อเดียว</p>
              <p><strong className="text-foreground">NXDOMAIN</strong> ตอบว่าไม่พบโดเมน (ไม่ส่งต่อไป upstream) ส่วน <strong className="text-foreground">Sinkhole</strong> ตอบที่อยู่ 0.0.0.0/:: แทน — เหมาะกับกรณีที่ต้องการให้เบราว์เซอร์จัดการ error ได้ดีกว่า NXDOMAIN</p>
              <p>ถ้าเคยสร้าง "โซนเปล่า" (ไม่มี records) ไว้เพื่อบล็อกโดเมนมาก่อน แนะนำให้ย้ายมาเพิ่มในแท็บนี้แทน แล้วลบโซนเปล่านั้นได้เอง</p>
            </div>
          </div>

          {emptyZoneSuggestions.length > 0 && (
            <div className="flex gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-xs leading-relaxed text-warning">
              <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
              <div>
                <strong>พบโซนเปล่าที่อาจถูกสร้างไว้เพื่อบล็อกโดเมน (ไม่มี records):</strong>
                <ul className="mt-1 list-disc space-y-0.5 pl-4 font-mono">
                  {emptyZoneSuggestions.map(z => (
                    <li key={z.id}>{z.zoneName}</li>
                  ))}
                </ul>
                <p className="mt-1 font-sans">พิจารณาย้ายมาที่แท็บนี้ แล้วลบโซนเปล่าเหล่านั้นเองในแท็บ "Zones &amp; Records" (ระบบไม่ลบ/แปลงให้อัตโนมัติ)</p>
              </div>
            </div>
          )}
        </TabsContent>

        <TabsContent value="blocklists" className="space-y-4">
          {/* Permanent explainer — sinkhole vs nxdomain, and why the mode is
              chosen per-list, not per-domain (plan §2.1.2/§2.1.4). */}
          <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
            <Info className="mt-0.5 h-4 w-4 shrink-0" />
            <div className="space-y-1">
              <p>
                <strong className="text-foreground">Sinkhole</strong> ตอบที่อยู่ 0.0.0.0 / :: และ{" "}
                <strong className="text-foreground">บล็อกเฉพาะชื่อที่อยู่ในไฟล์เท่านั้น ไม่ครอบ subdomain</strong> — เร็วและเบาที่สุด เหมาะกับ list ขนาดใหญ่ (ค่าเริ่มต้น)
              </p>
              <p>
                <strong className="text-foreground">NXDOMAIN</strong> ตอบว่าไม่พบโดเมนและ{" "}
                <strong className="text-foreground">ครอบ subdomain ทั้งหมด</strong> แต่ dnsmasq ต้อง parse ไฟล์ทุกครั้งที่กด Apply DNS จึงมีเพดานโดเมนต่ำกว่า
                (สูงสุด {DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS.toLocaleString()} โดเมนรวมทุก list ที่ใช้โหมดนี้) ทำให้ Apply ช้าลง และต้องใช้ dnsmasq เวอร์ชัน 2.86 ขึ้นไป
              </p>
              <p>
                เลือกโหมดได้เฉพาะ<strong className="text-foreground">ระดับ list</strong> ไม่ใช่รายโดเมน — ถ้าต้องการกำหนดโหมดเป็นรายโดเมน ให้ใช้แท็บ{" "}
                <button
                  type="button"
                  onClick={() => setActiveTab("blocked")}
                  className="cursor-pointer text-primary underline underline-offset-2"
                >
                  Blocked Domains
                </button>{" "}
                แทน (จำกัด {DNS_BLOCKED_DOMAINS_MAX.toLocaleString()} รายการ แต่เลือกโหมดต่อโดเมนได้)
              </p>
            </div>
          </div>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Download className="h-4 w-4 text-muted-foreground" />
                Blocklists
                <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                  {blocklists.length}/{DNS_BLOCKLISTS_MAX}
                </Badge>
              </CardTitle>
              <Button
                onClick={openCreateBlocklistModal}
                size="sm"
                disabled={blocklists.length >= DNS_BLOCKLISTS_MAX}
                className="cursor-pointer gap-1.5 font-semibold"
              >
                <Plus className="h-4 w-4" />
                Add Blocklist
              </Button>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* Summary bar — total domains across every list/mode vs. the
                  overall cap, plus the narrower nxdomain-only total vs. its
                  own (lower) cap. */}
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="rounded-lg border border-border bg-muted/50 p-3">
                  <p className="text-[10px] font-medium text-muted-foreground">รวมทุก List / ทุกโหมด</p>
                  <p className="font-mono text-sm font-semibold text-foreground">
                    {blocklistTotalDomains.toLocaleString()} <span className="text-muted-foreground">/ {DNS_BLOCKLIST_TOTAL_DOMAINS_MAX.toLocaleString()} โดเมน</span>
                  </p>
                </div>
                <div className="rounded-lg border border-border bg-muted/50 p-3">
                  <p className="text-[10px] font-medium text-muted-foreground">เฉพาะโหมด NXDOMAIN</p>
                  <p className="font-mono text-sm font-semibold text-foreground">
                    {blocklistNXDomainDomains.toLocaleString()} <span className="text-muted-foreground">/ {DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS.toLocaleString()} โดเมน</span>
                  </p>
                </div>
              </div>

              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="w-[18%] text-xs font-medium text-muted-foreground">Name</TableHead>
                    <TableHead className="w-[18%] text-xs font-medium text-muted-foreground">Source</TableHead>
                    <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Mode</TableHead>
                    <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Domains</TableHead>
                    <TableHead className="w-[16%] text-xs font-medium text-muted-foreground">Last fetched</TableHead>
                    <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Status</TableHead>
                    <TableHead className="w-[8%] text-xs font-medium text-muted-foreground">Enabled</TableHead>
                    <TableHead className="w-[10%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {blocklists.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={8} className="py-8 text-center text-xs text-muted-foreground">
                        ยังไม่มี Blocklist — กด "Add Blocklist" เพื่อ subscribe URL หรืออัปโหลดไฟล์ hosts
                      </TableCell>
                    </TableRow>
                  ) : (
                    blocklists.map((b) => {
                      const busy = blocklistBusyIds.has(b.id)
                      return (
                        <TableRow key={b.id}>
                          <TableCell className="py-3">
                            <span className="text-xs font-medium text-foreground">{b.name}</span>
                          </TableCell>
                          <TableCell className="py-3">
                            <div className="flex items-center gap-1.5">
                              <Badge
                                variant="outline"
                                className="rounded border-border bg-muted px-1.5 py-0 text-[10px] font-medium text-muted-foreground"
                              >
                                {b.sourceType === "url" ? "URL" : "Upload"}
                              </Badge>
                              {b.sourceType === "url" && b.url && (
                                <span className="max-w-[140px] truncate font-mono text-[10px] text-muted-foreground" title={b.url}>
                                  {blocklistUrlHost(b.url)}
                                </span>
                              )}
                            </div>
                          </TableCell>
                          <TableCell className="py-3">
                            <Badge
                              variant="outline"
                              className={`rounded px-1.5 py-0 text-[10px] font-medium ${b.blockMode === "sinkhole"
                                ? "border-warning/30 bg-warning/10 text-warning"
                                : "border-primary/20 bg-primary/10 text-primary"
                                }`}
                            >
                              {b.blockMode === "sinkhole" ? "Sinkhole" : "NXDOMAIN"}
                            </Badge>
                          </TableCell>
                          <TableCell className="py-3 font-mono text-xs text-foreground">
                            {b.domainCount.toLocaleString()}
                          </TableCell>
                          <TableCell className="py-3 text-xs text-muted-foreground">
                            {formatBlocklistTime(b.lastFetchedAt)}
                          </TableCell>
                          <TableCell className="py-3">
                            {b.lastError ? (
                              <Badge variant="destructive" className="rounded px-1.5 py-0 text-[10px] font-medium" title={b.lastError}>
                                Error
                              </Badge>
                            ) : (
                              <Badge
                                variant="outline"
                                className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-medium text-primary"
                              >
                                OK
                              </Badge>
                            )}
                          </TableCell>
                          <TableCell className="py-3">
                            <Switch
                              size="sm"
                              checked={b.enabled}
                              disabled={busy}
                              onCheckedChange={() => handleToggleBlocklist(b.id)}
                              className="cursor-pointer"
                            />
                          </TableCell>
                          <TableCell className="py-3 text-right">
                            <div className="flex items-center justify-end gap-2">
                              {b.sourceType === "url" && (
                                <Button
                                  variant="outline"
                                  size="icon-sm"
                                  disabled={busy}
                                  onClick={() => handleRefreshBlocklist(b.id)}
                                  className="cursor-pointer text-muted-foreground hover:text-foreground"
                                  title="Refresh (re-fetch จาก URL)"
                                >
                                  <RefreshCw className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} />
                                </Button>
                              )}
                              <Button
                                variant="outline"
                                size="icon-sm"
                                disabled={busy}
                                onClick={() => openEditBlocklistModal(b)}
                                className="cursor-pointer text-muted-foreground hover:text-foreground"
                                title="แก้ไข"
                              >
                                <Edit className="h-4 w-4" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                disabled={busy}
                                onClick={() => handleDeleteBlocklist(b.id, b.name)}
                                className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                title="ลบ"
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      )
                    })
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="settings" className="space-y-4">
          {/* Listen Interfaces — real interfaces from Interface Service, independent of DHCP Server */}
          <Card>
            <CardHeader className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Network className="h-4 w-4 text-muted-foreground" />
                DNS Server Listen Interfaces
                {isSavingInterfaces && (
                  <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
                )}
              </CardTitle>
              <CardDescription className="text-xs">
                เลือก Interface จริงที่มีอยู่ในเครื่อง (ดึงจาก Interface Service) ที่ต้องการให้ DNS Server (dnsmasq) เปิดรับคำขอ DNS — ค่านี้แยกอิสระจากการตั้งค่า DHCP Server
              </CardDescription>
            </CardHeader>
            <CardContent>
              {availableInterfaces.length === 0 && missingInterfaces.length === 0 ? (
                <p className="text-xs italic text-muted-foreground">ไม่พบ Interface ที่มี Role เป็น LAN ในระบบ</p>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {availableInterfaces.map(iface => {
                    const checked = selectedInterfaces.includes(iface.name)
                    return (
                      <label
                        key={iface.id}
                        className={`flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 font-mono text-xs transition ${
                          checked
                            ? "border-primary/40 bg-primary/10 text-foreground"
                            : "border-border bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground"
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={checked}
                          disabled={isSavingInterfaces}
                          onChange={(e) => handleToggleInterface(iface.name, e.target.checked)}
                          className="h-4 w-4 cursor-pointer accent-primary"
                        />
                        {ifaceLabel(iface)}
                      </label>
                    )
                  })}
                  {missingInterfaces.map(name => (
                    <label
                      key={`missing-${name}`}
                      className="flex cursor-pointer items-center gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 font-mono text-xs text-foreground transition"
                    >
                      <input
                        type="checkbox"
                        checked
                        disabled={isSavingInterfaces}
                        onChange={() => handleToggleInterface(name, false)}
                        className="h-4 w-4 cursor-pointer accent-destructive"
                      />
                      {name}
                      <Badge variant="destructive">Missing</Badge>
                    </label>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Upstream Resolvers (DNS Server) — docs/ref/todo/
              dns-server-settings-tab-and-upstream-plan.md T-12 item 2 */}
          <Card>
            <CardHeader className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Globe className="h-4 w-4 text-muted-foreground" />
                Upstream Resolvers (DNS Server)
              </CardTitle>
              <CardDescription className="text-xs">
                กำหนดปลายทางที่ DNS Server (dnsmasq) จะส่งคิวรีต่อไปเมื่อไม่สามารถตอบเองได้ — แยกอิสระจากการตั้งค่า System DNS
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {upstreamError && (
                <Alert variant="destructive" className="px-3 py-2.5">
                  <AlertCircle className="h-4 w-4" />
                  <AlertDescription className="text-xs">{upstreamError}</AlertDescription>
                </Alert>
              )}

              <div className="space-y-1.5">
                <Label className="block text-xs font-medium text-muted-foreground">
                  แหล่งที่มาของ Upstream DNS
                </Label>
                <Select value={upstreamMode} onValueChange={(v) => handleUpstreamModeChange(v as "system" | "custom")}>
                  <SelectTrigger className="h-9 w-full max-w-sm text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="system" className="text-xs">ใช้ตาม System DNS (ค่าเริ่มต้น)</SelectItem>
                    <SelectItem value="custom" className="text-xs">กำหนดเอง</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {upstreamMode === "system" ? (
                <div className="space-y-2 rounded-lg border border-border bg-muted/50 p-3">
                  <Label className="text-xs font-semibold text-foreground">ค่าที่จะถูกใช้จริง (อ่านจาก System DNS)</Label>
                  {effectiveSystemUpstreams.length === 0 ? (
                    <p className="text-xs italic text-muted-foreground">ยังไม่มีค่า System DNS ที่ใช้ได้ในขณะนี้</p>
                  ) : (
                    <ul className="space-y-1 font-mono text-xs text-foreground">
                      {effectiveSystemUpstreams.map((ip, idx) => (
                        <li key={idx} className="flex items-center gap-1.5">
                          <Check className="h-3 w-3 shrink-0 text-primary" />
                          {ip}
                        </li>
                      ))}
                    </ul>
                  )}
                  <p className="text-[10px] text-muted-foreground">
                    ดูหรือแก้ไขได้ที่หน้า <Link to="/dns" className="text-primary underline underline-offset-2">System DNS</Link>
                  </p>
                  <p className="text-[10px] font-medium text-warning">
                    การแก้ค่าในหน้า System DNS จะไม่อัปเดต DNS Server ให้อัตโนมัติอีกต่อไป — กด "Apply DNS Zones" เพื่อให้ค่าใหม่มีผลกับเครื่องลูก
                  </p>
                </div>
              ) : (
                <div className="space-y-2 rounded-lg border border-border bg-muted/50 p-3">
                  <Label className="text-xs font-semibold text-foreground">Upstream DNS Server (สูงสุด {DNS_UPSTREAM_MAX_SERVERS} รายการ)</Label>
                  <div className="space-y-2">
                    {upstreamServers.length === 0 && (
                      <p className="text-xs italic text-muted-foreground">ยังไม่มีรายการ — กดปุ่ม "เพิ่ม" ด้านล่าง</p>
                    )}
                    {upstreamServers.map((ip, idx) => (
                      <div key={idx} className="flex items-center gap-2">
                        <Input
                          type="text"
                          value={ip}
                          onChange={(e) => handleUpstreamServerChange(idx, e.target.value)}
                          placeholder="เช่น 1.1.1.1"
                          className="h-9 font-mono text-sm"
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleRemoveUpstreamServer(idx)}
                          className="cursor-pointer shrink-0 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                          title="ลบรายการนี้"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    ))}
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleAddUpstreamServer}
                    disabled={upstreamServers.length >= DNS_UPSTREAM_MAX_SERVERS}
                    className="cursor-pointer gap-1.5"
                  >
                    <Plus className="h-4 w-4" />
                    เพิ่ม
                  </Button>
                  <p className="text-[10px] text-muted-foreground">
                    รับเฉพาะหมายเลข IP (ไม่รองรับ port/ชื่อโฮสต์/DoH/DoT)
                  </p>
                  <p className="text-[10px] font-medium text-warning">
                    การแก้ System DNS จะไม่มีผลกับ DNS Server อีกต่อไป
                  </p>
                </div>
              )}

              <p className="text-[10px] text-muted-foreground">
                บันทึกแล้วจะ <span className="font-semibold text-warning">restart dnsmasq</span> (DNS/DHCP สะดุดสั้น ๆ)
              </p>
              <Button
                onClick={handleSaveUpstream}
                disabled={isSavingUpstream}
                size="sm"
                variant="outline"
                className="cursor-pointer gap-1.5"
              >
                {isSavingUpstream ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                บันทึก Upstream Resolvers
              </Button>
            </CardContent>
          </Card>

          {/* DNS Statistics — query logging switch + reverse-cache TTL/cap
              (docs/ref/todo/statistics-dns-top-domain-plan.md T-13) */}
          <Card>
            <CardHeader className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Info className="h-4 w-4 text-muted-foreground" />
                DNS Statistics (สถิติ DNS)
              </CardTitle>
              <CardDescription className="text-xs">
                ควบคุมการเก็บสถิติ "Top Queried Domains" และการเติมชื่อโดเมนให้ IP ปลายทางในหน้า Statistics
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between gap-4 rounded-lg border border-border bg-muted/50 p-3">
                <div className="space-y-0.5">
                  <Label className="text-xs font-semibold text-foreground">
                    เก็บสถิติ DNS query (Top Queried Domains + ชื่อโดเมนของ IP ปลายทาง)
                  </Label>
                  <p className="text-[10px] text-muted-foreground">
                    เมื่อเปิด จะเห็นรายชื่อโดเมนที่เครื่องในบ้านถามได้ในหน้า Statistics — โปรดพิจารณาความเป็นส่วนตัวก่อนเปิด
                    การเปิด/ปิดสวิตช์นี้จะ <span className="font-semibold text-warning">restart dnsmasq</span> (DNS/DHCP สะดุดสั้น ๆ)
                  </p>
                </div>
                <Switch
                  checked={queryLogging}
                  onCheckedChange={handleToggleQueryLogging}
                  disabled={isSavingQueryLogging}
                  className="cursor-pointer"
                />
              </div>

              <div className="space-y-3 rounded-lg border border-border bg-muted/50 p-3">
                {dnsCacheLimitsError && (
                  <Alert variant="destructive" className="px-3 py-2.5">
                    <AlertCircle className="h-4 w-4" />
                    <AlertDescription className="text-xs">{dnsCacheLimitsError}</AlertDescription>
                  </Alert>
                )}
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="dns-cache-ttl" className="block text-xs font-medium text-muted-foreground">
                      อายุของ mapping IP→โดเมน (นาที)
                    </Label>
                    <Input
                      id="dns-cache-ttl"
                      type="number"
                      min={DNS_CACHE_TTL_MIN}
                      max={DNS_CACHE_TTL_MAX}
                      value={dnsCacheTtlInput}
                      onChange={(e) => setDnsCacheTtlInput(e.target.value)}
                      className="h-9 font-mono text-sm"
                    />
                    <p className="text-[10px] text-muted-foreground">
                      ค่าเริ่มต้น {DNS_CACHE_TTL_DEFAULT} นาที (ช่วง {DNS_CACHE_TTL_MIN}-{DNS_CACHE_TTL_MAX})
                    </p>
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="dns-cache-max" className="block text-xs font-medium text-muted-foreground">
                      จำนวน mapping สูงสุด
                    </Label>
                    <Input
                      id="dns-cache-max"
                      type="number"
                      min={DNS_CACHE_ENTRIES_MIN}
                      max={DNS_CACHE_ENTRIES_MAX}
                      value={dnsCacheMaxInput}
                      onChange={(e) => setDnsCacheMaxInput(e.target.value)}
                      className="h-9 font-mono text-sm"
                    />
                    <p className="text-[10px] text-muted-foreground">
                      ค่าเริ่มต้น {DNS_CACHE_ENTRIES_DEFAULT} รายการ (ช่วง {DNS_CACHE_ENTRIES_MIN}-{DNS_CACHE_ENTRIES_MAX})
                    </p>
                  </div>
                </div>
                <p className="text-[10px] text-muted-foreground">
                  ปรับแล้วมีผลทันที ไม่ต้อง restart บริการใด ๆ — ค่าสูงขึ้น = ชื่อโดเมนครบขึ้นแต่ใช้ RAM มากขึ้น /
                  ค่ายาวขึ้น = เสี่ยงเห็นชื่อเก่าที่ IP ถูกใช้ซ้ำแล้ว
                </p>
                <Button
                  onClick={handleSaveCacheLimits}
                  disabled={isSavingCacheLimits}
                  size="sm"
                  variant="outline"
                  className="cursor-pointer gap-1.5"
                >
                  {isSavingCacheLimits ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                  บันทึกค่า Cache
                </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Dialogs live outside <Tabs> so Radix unmounting the inactive
          TabsContent never drops their open/edit state (T-12 item 1, plan §5
          item 10). */}

      {/* Zone Add/Edit Dialog Modal */}
      <Drawer direction="right" open={isZoneModalOpen} onOpenChange={setIsZoneModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[450px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingZone ? "แก้ไข DNS Zone" : "เพิ่ม DNS Zone ใหม่"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveZone} className="space-y-4 text-sm">
            {zoneError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{zoneError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Zone Name */}
            <div className="space-y-1.5">
              <Label htmlFor="zone-name" className="block text-xs font-medium text-muted-foreground">
                Zone Name / Local Domain (ชื่อโซน/โดเมน) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="zone-name"
                type="text"
                required
                value={zoneName}
                onChange={(e) => setZoneName(e.target.value)}
                placeholder="เช่น office.local หรือ internal.net"
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Field: Authoritative Toggle */}
            <div className="flex items-center justify-between rounded-lg border border-border bg-muted/50 p-3">
              <div className="space-y-0.5">
                <Label className="text-xs font-semibold text-foreground">Local Zone (ตอบจาก records ในระบบ)</Label>
                <p className="text-[10px] text-muted-foreground">
                  เปิดหากต้องการกำหนด DNS Records เอง หรือปิดหากต้องการทำ DNS Forwarding
                </p>
              </div>
              <Switch
                checked={isAuthoritative}
                onCheckedChange={setIsAuthoritative}
                className="cursor-pointer"
              />
            </div>

            {/* Field: Forward To (Conditional) */}
            {!isAuthoritative && (
              <div className="animate-slide-in space-y-1.5">
                <Label htmlFor="forward-ip" className="block text-xs font-medium text-muted-foreground">
                  Forward To Upstream IP (ส่งต่อไปที่เซิร์ฟเวอร์) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="forward-ip"
                  type="text"
                  required
                  value={forwardTo}
                  onChange={(e) => setForwardTo(e.target.value)}
                  placeholder="เช่น 8.8.8.8 หรือ 1.1.1.1"
                  className="h-9 font-mono text-sm"
                />
              </div>
            )}

            {/* Field: Allowed Client IPs */}
            <div className="space-y-1.5">
              <Label htmlFor="allowed-ips" className="block text-xs font-medium text-muted-foreground">
                Allowed Client IPs (กลุ่มไอพีที่อนุญาตคิวรี)
              </Label>
              <Input
                id="allowed-ips"
                type="text"
                value={allowedIps}
                onChange={(e) => setAllowedIps(e.target.value)}
                placeholder="ระบุ any หรือคั่นด้วยลูกน้ำ เช่น 192.168.1.0/24"
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsZoneModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSaving}
                className="cursor-pointer px-6 font-semibold"
              >
                {isSaving ? "Saving..." : editingZone ? "Save Changes" : "Create Zone"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>

      {/* 5. Record Add/Edit Drawer */}
      <Drawer direction="right" open={isRecModalOpen} onOpenChange={setIsRecModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[450px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingRecord ? "แก้ไข DNS Record" : `เพิ่ม DNS Record ในโซน ${selectedZone?.zoneName}`}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveRecord} className="space-y-4 text-sm">
            {recError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{recError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Host Name */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-name" className="block text-xs font-medium text-muted-foreground">
                Host Name (ชื่อโดเมนย่อย)
              </Label>
              <div className="flex items-center gap-1.5">
                <Input
                  id="rec-name"
                  type="text"
                  value={recName}
                  onChange={(e) => setRecName(e.target.value)}
                  placeholder="@ หรือเว้นว่าง หรือโดเมนย่อย เช่น printer"
                  className="h-9 flex-1 font-mono text-sm"
                />
                <span className="font-mono text-xs font-semibold text-muted-foreground">
                  .{selectedZone?.zoneName}
                </span>
              </div>
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                ใส่ @ หรือเว้นว่าง หากต้องการให้ชี้ไปที่ตัวโดเมนหลักโดยตรง ({selectedZone?.zoneName})
              </p>
            </div>

            {/* Field: Record Type */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-type" className="block text-xs font-medium text-muted-foreground">
                Record Type (ประเภทระเบียน)
              </Label>
              <select
                id="rec-type"
                value={recType}
                onChange={(e) => setRecType(e.target.value)}
                className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
              >
                <option value="A">A (Address)</option>
                <option value="AAAA">AAAA (IPv6 Address)</option>
                <option value="CNAME">CNAME (Alias)</option>
                <option value="MX">MX (Mail Exchange)</option>
                <option value="TXT">TXT (Text)</option>
                <option value="PTR">PTR (Pointer)</option>
                <option value="NS">NS (Name Server)</option>
              </select>
            </div>

            {/* Field: Value */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-val" className="block text-xs font-medium text-muted-foreground">
                Record Value (ข้อมูลระเบียน) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="rec-val"
                type="text"
                required
                value={recValue}
                onChange={(e) => setRecValue(e.target.value)}
                placeholder={
                  recType === "A"
                    ? "เช่น 192.168.1.15"
                    : recType === "CNAME"
                      ? "เช่น pigate.local"
                      : recType === "MX"
                        ? "ระบุลำดับความสำคัญและชื่อเซิร์ฟเวอร์ เช่น 10 mail.example.com"
                        : recType === "NS"
                          ? "เช่น ns1.example.com"
                          : "ค่าระเบียนตามประเภท"
                }
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Field: TTL */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-ttl" className="block text-xs font-medium text-muted-foreground">
                TTL (Seconds) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="rec-ttl"
                type="number"
                required
                min="30"
                value={recTtl}
                onChange={(e) => setRecTtl(e.target.value)}
                placeholder="300"
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsRecModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSaving}
                className="cursor-pointer px-6 font-semibold"
              >
                {isSaving ? "Saving..." : editingRecord ? "Save Record" : "Create Record"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>

      {/* Blocked Domain Add/Edit Drawer */}
      <Drawer direction="right" open={isBlockedModalOpen} onOpenChange={setIsBlockedModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[450px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingBlockedDomain ? "แก้ไขโดเมนที่ถูกบล็อก" : "เพิ่มโดเมนที่ต้องการบล็อก"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveBlockedDomain} className="space-y-4 text-sm">
            {blkError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{blkError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Domain */}
            <div className="space-y-1.5">
              <Label htmlFor="blk-domain" className="block text-xs font-medium text-muted-foreground">
                Domain (โดเมนที่ต้องการบล็อก) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="blk-domain"
                type="text"
                required
                value={blkDomain}
                onChange={(e) => setBlkDomain(e.target.value)}
                placeholder="เช่น ads.example.com"
                className="h-9 font-mono text-sm"
              />
              <p className="text-[10px] text-muted-foreground">
                บล็อกครอบคลุมโดเมนย่อยทั้งหมดโดยอัตโนมัติ (www.ads.example.com ก็จะถูกบล็อกด้วย)
              </p>
            </div>

            {/* Field: Mode */}
            <div className="space-y-1.5">
              <Label htmlFor="blk-mode" className="block text-xs font-medium text-muted-foreground">
                Mode (วิธีตอบกลับ)
              </Label>
              <Select value={blkMode} onValueChange={(v) => setBlkMode(v as "nxdomain" | "sinkhole")}>
                <SelectTrigger id="blk-mode" className="h-9 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="nxdomain" className="text-xs">NXDOMAIN (ค่าเริ่มต้น — ไม่พบโดเมน)</SelectItem>
                  <SelectItem value="sinkhole" className="text-xs">Sinkhole (ตอบ 0.0.0.0 / ::)</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* Field: Comment */}
            <div className="space-y-1.5">
              <Label htmlFor="blk-comment" className="block text-xs font-medium text-muted-foreground">
                Comment (หมายเหตุ)
              </Label>
              <Input
                id="blk-comment"
                type="text"
                value={blkComment}
                onChange={(e) => setBlkComment(e.target.value)}
                placeholder="เช่น ad network"
                maxLength={128}
                className="h-9 text-sm"
              />
            </div>

            {/* Field: Enabled */}
            <div className="flex items-center justify-between rounded-lg border border-border bg-muted/50 p-3">
              <div className="space-y-0.5">
                <Label className="text-xs font-semibold text-foreground">Enabled</Label>
                <p className="text-[10px] text-muted-foreground">ปิดเพื่อพักการบล็อกชั่วคราวโดยไม่ต้องลบรายการ</p>
              </div>
              <Switch
                checked={blkEnabled}
                onCheckedChange={setBlkEnabled}
                className="cursor-pointer"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsBlockedModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSavingBlocked}
                className="cursor-pointer px-6 font-semibold"
              >
                {isSavingBlocked ? "Saving..." : editingBlockedDomain ? "Save Changes" : "Add Domain"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>

      {/* Blocklist Add/Edit Drawer */}
      <Drawer direction="right" open={isBlocklistModalOpen} onOpenChange={(open) => !open && closeBlocklistModal()}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[450px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="flex items-center gap-2 text-base font-semibold">
              <Download className="h-4 w-4 text-muted-foreground" />
              {editingBlocklist ? "แก้ไข Blocklist" : "เพิ่ม Blocklist ใหม่"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveBlocklist} className="space-y-4 text-sm">
            {blstError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{blstError}</AlertDescription>
              </Alert>
            )}

            {/* Source type toggle — create only; fixed once created */}
            {!editingBlocklist ? (
              <div className="space-y-1.5">
                <Label className="block text-xs font-medium text-muted-foreground">แหล่งที่มา</Label>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant={blstSourceType === "url" ? "default" : "outline"}
                    size="sm"
                    onClick={() => setBlstSourceType("url")}
                    className="flex-1 cursor-pointer gap-1.5 font-semibold"
                  >
                    <Globe className="h-4 w-4" />
                    Subscribe URL
                  </Button>
                  <Button
                    type="button"
                    variant={blstSourceType === "upload" ? "default" : "outline"}
                    size="sm"
                    onClick={() => setBlstSourceType("upload")}
                    className="flex-1 cursor-pointer gap-1.5 font-semibold"
                  >
                    <Upload className="h-4 w-4" />
                    Upload file
                  </Button>
                </div>
              </div>
            ) : (
              <div className="space-y-1.5">
                <Label className="block text-xs font-medium text-muted-foreground">แหล่งที่มา</Label>
                <Badge variant="outline" className="rounded border-border bg-muted px-1.5 py-0 text-[10px] font-medium text-muted-foreground">
                  {editingBlocklist.sourceType === "url" ? "Subscribe URL" : "Upload file"} (แก้ไขไม่ได้)
                </Badge>
              </div>
            )}

            {/* Field: Name */}
            <div className="space-y-1.5">
              <Label htmlFor="blst-name" className="block text-xs font-medium text-muted-foreground">
                Name (ชื่อ Blocklist) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="blst-name"
                type="text"
                required
                value={blstName}
                onChange={(e) => setBlstName(e.target.value)}
                placeholder="เช่น StevenBlack unified"
                className="h-9 text-sm"
              />
            </div>

            {/* Field: URL (subscribe URL mode only) */}
            {blstSourceType === "url" && (
              <div className="space-y-1.5">
                <Label htmlFor="blst-url" className="block text-xs font-medium text-muted-foreground">
                  Subscribe URL <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="blst-url"
                  type="text"
                  required
                  value={blstUrl}
                  onChange={(e) => setBlstUrl(e.target.value)}
                  placeholder="https://raw.githubusercontent.com/..."
                  className="h-9 font-mono text-xs"
                />
                <p className="text-[10px] text-muted-foreground">
                  ต้องขึ้นต้นด้วย <span className="font-mono">https://</span> เท่านั้น — <strong className="text-foreground">URL ภายใน LAN ใช้ไม่ได้</strong> (มาตรการความปลอดภัยป้องกัน SSRF)
                  {!editingBlocklist && " ระบบจะดึงและตรวจสอบไฟล์ทันทีหลังกด Create"}
                </p>
              </div>
            )}

            {/* Field: File upload (upload mode, create only) */}
            {!editingBlocklist && blstSourceType === "upload" && (
              <div className="space-y-1.5">
                <Label htmlFor="blst-file" className="block text-xs font-medium text-muted-foreground">
                  Hosts File <span className="text-destructive">*</span>
                </Label>
                <input
                  id="blst-file"
                  type="file"
                  accept=".txt,.hosts,text/plain"
                  onChange={(e) => {
                    const f = e.target.files && e.target.files.length > 0 ? e.target.files[0] : null
                    setBlstFile(f)
                  }}
                  className="w-full cursor-pointer rounded-lg border border-border text-xs text-muted-foreground file:mr-3 file:cursor-pointer file:rounded-l-lg file:border-0 file:border-r file:border-border file:bg-primary file:px-3 file:py-1.5 file:text-xs file:font-semibold file:text-primary-foreground"
                />
                {blstFile && (
                  <p className="text-[10px] text-muted-foreground">
                    ไฟล์ที่เลือก: {blstFile.name} ({(blstFile.size / (1024 * 1024)).toFixed(2)} MB)
                    {blstFile.size > DNS_BLOCKLIST_MAX_FILE_MB * 1024 * 1024 && (
                      <span className="font-semibold text-destructive"> — เกินขนาดสูงสุด {DNS_BLOCKLIST_MAX_FILE_MB} MB</span>
                    )}
                  </p>
                )}
                <p className="text-[10px] text-muted-foreground">
                  รองรับไฟล์รูปแบบ hosts (.txt / .hosts) ขนาดไม่เกิน {DNS_BLOCKLIST_MAX_FILE_MB} MB — ระบบจะทิ้งคอลัมน์ IP ต้นฉบับและ render ใหม่เองทั้งหมด (ป้องกัน DNS spoofing)
                </p>
              </div>
            )}

            {/* Field: Block mode — same pattern as the deny-list's blk-mode
                Select above, but default/order swapped (T-12 item 3b). */}
            <div className="space-y-1.5">
              <Label htmlFor="blst-mode" className="block text-xs font-medium text-muted-foreground">
                Block mode (วิธีตอบกลับ)
              </Label>
              <Select value={blstMode} onValueChange={(v) => setBlstMode(v as "sinkhole" | "nxdomain")}>
                <SelectTrigger id="blst-mode" className="h-9 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="sinkhole" className="text-xs">Sinkhole (ค่าเริ่มต้น — ตอบ 0.0.0.0 / ::)</SelectItem>
                  <SelectItem value="nxdomain" className="text-xs">NXDOMAIN (ตอบว่าไม่พบโดเมน — ครอบ subdomain ด้วย)</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-[10px] text-muted-foreground">
                เปลี่ยนโหมดไม่ต้องดึงข้อมูลใหม่ แต่ต้องกด "Apply DNS Zones" เพื่อให้มีผลจริง
              </p>
            </div>

            {/* Field: Enabled */}
            <div className="flex items-center justify-between rounded-lg border border-border bg-muted/50 p-3">
              <div className="space-y-0.5">
                <Label className="text-xs font-semibold text-foreground">Enabled</Label>
                <p className="text-[10px] text-muted-foreground">ปิดเพื่อพักการบล็อกของ list นี้ชั่วคราวโดยไม่ต้องลบ</p>
              </div>
              <Switch
                checked={blstEnabled}
                onCheckedChange={setBlstEnabled}
                className="cursor-pointer"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={closeBlocklistModal}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSavingBlocklist}
                className="cursor-pointer gap-1.5 px-6 font-semibold"
              >
                {isSavingBlocklist ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    {editingBlocklist ? "Saving..." : blstSourceType === "url" ? "Fetching..." : "Uploading..."}
                  </>
                ) : (
                  editingBlocklist ? "Save Changes" : "Create"
                )}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
