// Types for Dashboard
export interface FirewallLog {
  id: string
  time: string
  action: "PASS" | "DROP"
  src: string
  dest: string
  port: string
  proto: string
  reason: string
}

// Initial mockup logs for Dashboard
export const initialFirewallLogs: FirewallLog[] = [
  {
    id: "log-1",
    time: "14:31:02",
    action: "DROP",
    src: "185.220.101.4",
    dest: "10.0.0.45",
    port: "445",
    proto: "TCP",
    reason: "Blocked Port (SMB)"
  },
  {
    id: "log-2",
    time: "14:31:15",
    action: "PASS",
    src: "192.168.1.105",
    dest: "8.8.8.8",
    port: "53",
    proto: "UDP",
    reason: "DNS request"
  },
  {
    id: "log-3",
    time: "14:31:22",
    action: "DROP",
    src: "192.168.1.132",
    dest: "203.0.113.5",
    port: "23",
    proto: "TCP",
    reason: "Blocked Telnet"
  },
  {
    id: "log-4",
    time: "14:31:30",
    action: "PASS",
    src: "192.168.1.100",
    dest: "142.250.196.46",
    port: "443",
    proto: "TCP",
    reason: "HTTPS traffic"
  },
  {
    id: "log-5",
    time: "14:31:40",
    action: "DROP",
    src: "45.143.203.14",
    dest: "10.0.0.45",
    port: "22",
    proto: "TCP",
    reason: "Brute-force SSH"
  }
]

// Mockup options for Dashboard log streaming generator
export const mockSources = [
  "192.168.1.104",
  "192.168.1.112",
  "192.168.1.188",
  "185.220.101.4",
  "45.143.203.18",
  "82.102.23.140",
  "192.168.1.101"
]

export const mockDestinations = [
  "8.8.8.8",
  "1.1.1.1",
  "10.0.0.45",
  "142.250.196.46",
  "151.101.1.140",
  "192.168.1.1"
]

export const mockLogServices = [
  { port: "53", proto: "UDP", reason: "DNS query", action: "PASS" },
  { port: "443", proto: "TCP", reason: "HTTPS secure", action: "PASS" },
  { port: "80", proto: "TCP", reason: "HTTP plain", action: "PASS" },
  { port: "22", proto: "TCP", reason: "Blocked SSH", action: "DROP" },
  { port: "23", proto: "TCP", reason: "Blocked Telnet", action: "DROP" },
  { port: "445", proto: "TCP", reason: "Blocked Port (SMB)", action: "DROP" },
  { port: "3389", proto: "TCP", reason: "RDP connection attempt", action: "DROP" }
]

// Types for Firewall Policy
export interface PolicyRule {
  id: string
  name: string
  inInterface: string // e.g. "ALL", "eth0", "wlan0"
  outInterface: string // e.g. "ALL", "eth0", "wlan0"
  source: string[]
  destination: string[]
  service: string[]
  action: "ACCEPT" | "DROP"
  log: boolean
  status: boolean // true = Enabled, false = Disabled
}

// Initial mockup rules for Firewall Policy
export const initialPolicyRules: PolicyRule[] = []

// Types for Address Objects
export interface AddressObject {
  id: string
  name: string
  type: "subnet" | "range" | "fqdn"
  value: string
  system: boolean
  refPolicies: string[]
}

// Initial mockup data for Address Objects
export const initialAddressObjects: AddressObject[] = [
  {
    id: "addr-1",
    name: "ALL",
    type: "subnet",
    value: "0.0.0.0/0",
    system: true,
    refPolicies: []
  },
  {
    id: "addr-2",
    name: "LAN_Network",
    type: "subnet",
    value: "192.168.1.0/24",
    system: false,
    refPolicies: []
  },
  {
    id: "addr-3",
    name: "Admin_PC",
    type: "subnet",
    system: false,
    value: "192.168.1.10/32",
    refPolicies: []
  },
  {
    id: "addr-4",
    name: "DHCP_Pool_Zone",
    type: "range",
    system: false,
    value: "192.168.1.100 - 192.168.1.200",
    refPolicies: []
  },
  {
    id: "addr-5",
    name: "Update_Server",
    type: "fqdn",
    system: false,
    value: "pigate-update.com",
    refPolicies: []
  },
  {
    id: "addr-6",
    name: "Malicious_IP_List",
    type: "subnet",
    system: false,
    value: "198.51.100.0/22",
    refPolicies: []
  }
]

// Types for Service Objects
export interface ServiceObject {
  id: string
  name: string
  protocol: "TCP" | "UDP" | "TCP/UDP" | "ICMP"
  port: string
  type: "system" | "custom"
  refPolicies: string[]
}

// Initial mockup data for Service Objects
export const initialServiceObjects: ServiceObject[] = [
  {
    id: "svc-1",
    name: "ALL",
    protocol: "TCP/UDP",
    port: "1-65535",
    type: "system",
    refPolicies: []
  },
  {
    id: "svc-2",
    name: "HTTP",
    protocol: "TCP",
    port: "80",
    type: "system",
    refPolicies: []
  },
  {
    id: "svc-3",
    name: "HTTPS",
    protocol: "TCP",
    port: "443",
    type: "system",
    refPolicies: []
  },
  {
    id: "svc-4",
    name: "SSH",
    protocol: "TCP",
    port: "22",
    type: "system",
    refPolicies: []
  },
  {
    id: "svc-5",
    name: "DNS",
    protocol: "UDP",
    port: "53",
    type: "system",
    refPolicies: []
  },
  {
    id: "svc-6",
    name: "ICMP",
    protocol: "ICMP",
    port: "-",
    type: "system",
    refPolicies: []
  }
]

// Types for Network Interfaces
export type AdminAccess = "HTTPS" | "HTTP" | "PING" | "SSH"
export type AddressingMode = "dhcp" | "static"
export type InterfaceType = "ethernet" | "wireless"

export interface WifiScanResult {
  ssid: string
  signal: number       // 0-100 percent
  security: string     // e.g. "WPA2-PSK", "WPA3", "Open"
  channel: number
  frequency: string    // "2.4 GHz" or "5 GHz"
}

export interface NetworkInterface {
  id: string
  name: string                // e.g. "eth0", "wlan0"
  alias: string               // e.g. "LAN_Internal", "WAN_WiFi"
  role: "LAN" | "WAN"
  type: InterfaceType
  subtype?: string            // e.g. "device", "veth", "bridge", "vlan"
  addressingMode: AddressingMode
  ip: string                  // e.g. "192.168.1.1"
  netmask: string             // e.g. "24"
  gateway: string             // e.g. "192.168.1.254" (used for static)
  metric?: number | null      // default-gateway route priority (lower = preferred, WAN failover); null/undefined = auto
  macAddress: string          // Effective MAC address currently active
  adminAccess: AdminAccess[]
  status: "up" | "down" | "offline"
  speed: string               // e.g. "1000 Mbps", "72 Mbps"
  // Wi-Fi specific
  wifiSSID?: string
  wifiPassword?: string       // masked
  wifiSecurity?: string       // e.g. "WPA2-PSK"
  // MAC Address Randomization & LAA support
  macMode?: "hardware" | "randomized" | "laa"
  realMacAddress?: string
  randomizedMac?: string
  laaMacAddress?: string
  randomizeOnReconnect?: boolean
  // Wi-Fi Backup & Failover Settings
  failoverEnabled?: boolean
  backupSsid?: string
  backupWifiPassword?: string
  backupWifiSecurity?: string
  ipCheckTimeout?: number
  primaryMaxRetries?: number
  failoverCooldown?: number
}

// Initial mockup data for Network Interfaces
export const initialNetworkInterfaces: NetworkInterface[] = [
  {
    id: "iface-1",
    name: "eth0",
    alias: "LAN_Internal",
    role: "LAN",
    type: "ethernet",
    subtype: "device",
    addressingMode: "static",
    ip: "192.168.1.1",
    netmask: "24",
    gateway: "",
    macAddress: "DC:A6:32:AA:BB:C1",
    realMacAddress: "DC:A6:32:AA:BB:C1",
    macMode: "hardware",
    adminAccess: ["PING", "HTTP", "SSH"],
    status: "up",
    speed: "1000 Mbps"
  },
  {
    id: "iface-2",
    name: "wlan0",
    alias: "WAN_WiFi",
    role: "WAN",
    type: "wireless",
    subtype: "device",
    addressingMode: "dhcp",
    ip: "10.0.0.45",
    netmask: "24",
    gateway: "10.0.0.1",
    metric: 100, // primary WAN priority
    macAddress: "4E:88:2F:BC:A1:90", // effective MAC
    realMacAddress: "DC:A6:32:AA:BB:C2", // hardware MAC
    macMode: "randomized",
    randomizedMac: "4E:88:2F:BC:A1:90",
    laaMacAddress: "9A:11:22:33:44:55",
    randomizeOnReconnect: true,
    adminAccess: ["PING"],
    status: "up",
    speed: "72 Mbps",
    wifiSSID: "MyHome_5G",
    wifiPassword: "••••••••",
    wifiSecurity: "WPA2",
    failoverEnabled: false,
    backupSsid: "MyHome_2G",
    backupWifiPassword: "backupPassword123",
    backupWifiSecurity: "WPA2",
    ipCheckTimeout: 15,
    primaryMaxRetries: 3,
    failoverCooldown: 60
  }
]

// Mock Wi-Fi Scan results
export const mockWifiScanResults: WifiScanResult[] = [
  { ssid: "MyHome_5G", signal: 85, security: "WPA2-PSK", channel: 36, frequency: "5 GHz" },
  { ssid: "MyHome_2G", signal: 72, security: "WPA2-PSK", channel: 6, frequency: "2.4 GHz" },
  { ssid: "Neighbor_AP", signal: 45, security: "WPA3", channel: 11, frequency: "2.4 GHz" },
  { ssid: "Cafe_Free_WiFi", signal: 30, security: "Open", channel: 1, frequency: "2.4 GHz" },
  { ssid: "Office_5G_Secured", signal: 62, security: "WPA2-Enterprise", channel: 149, frequency: "5 GHz" }
]

// Types for Static Routing
export interface StaticRoute {
  id: string
  destination: string     // e.g. "192.168.10.0/24"
  gateway: string         // e.g. "192.168.1.250" or "" for direct
  interface: string       // e.g. "eth0", "wlan0", "auto"
  metric: number          // priority (default 0 or 100)
  description: string
  status: boolean         // true = Active, false = Disabled
  type: "system" | "custom" | "defaultgateway" | "customgateway"
  scope: string
  src: string
  proto: string
  kernelOnly?: boolean
}

// Initial mockup data for Static Routes
export const initialStaticRoutes: StaticRoute[] = [
  {
    id: "route-1",
    destination: "0.0.0.0/0",
    gateway: "10.0.0.1",
    interface: "wlan0",
    metric: 100,
    description: "Default gateway route (WAN)",
    status: true,
    type: "system",
    scope: "global",
    src: "",
    proto: "boot"
  },
  {
    id: "route-2",
    destination: "192.168.1.0/24",
    gateway: "",
    interface: "eth0",
    metric: 0,
    description: "Direct subnet route for LAN",
    status: true,
    type: "system",
    scope: "link",
    src: "",
    proto: "kernel"
  },
  {
    id: "route-3",
    destination: "10.0.0.0/24",
    gateway: "",
    interface: "wlan0",
    metric: 0,
    description: "Direct subnet route for WAN",
    status: true,
    type: "system",
    scope: "link",
    src: "",
    proto: "kernel"
  }
]

// Types for DHCP Server
export interface DhcpConfig {
  id?: string
  enabled: boolean
  interface: string
  startIp: string
  endIp: string
  gateway: string
  netmask: string
  dns1: string
  dns2: string
  leaseTime: number // in seconds
}

export interface DhcpReservation {
  id: string
  deviceName: string
  macAddress: string
  ipAddress: string
}

export interface ActiveDhcpLease {
  id: string
  ipAddress: string
  macAddress: string
  hostname: string
  interface?: string
  expiresIn: string
  expiresAt?: string
}

// Initial mockup data for DHCP Server
export const initialDhcpConfigs: DhcpConfig[] = [
  {
    id: "dhcp-cfg-default",
    enabled: true,
    interface: "eth0",
    startIp: "192.168.1.100",
    endIp: "192.168.1.200",
    gateway: "192.168.1.1",
    netmask: "255.255.255.0",
    dns1: "8.8.8.8",
    dns2: "1.1.1.1",
    leaseTime: 86400 // 24 hours
  }
]

export const initialDhcpConfig: DhcpConfig = initialDhcpConfigs[0]

export const initialDhcpReservations: DhcpReservation[] = [
  {
    id: "res-1",
    deviceName: "CEO_Laptop",
    macAddress: "A1:B2:C3:D4:E5:F6",
    ipAddress: "192.168.1.10"
  },
  {
    id: "res-2",
    deviceName: "Network_Printer",
    macAddress: "11:22:33:44:55:66",
    ipAddress: "192.168.1.50"
  }
]

export const initialActiveDhcpLeases: ActiveDhcpLease[] = [
  {
    id: "lease-1",
    ipAddress: "192.168.1.101",
    macAddress: "99:88:77:66:55:44",
    hostname: "iPhone-13",
    interface: "eth0",
    expiresIn: "11 hours, 45 mins"
  },
  {
    id: "lease-2",
    ipAddress: "192.168.1.102",
    macAddress: "AA:BB:CC:DD:EE:FF",
    hostname: "Android-SmartTV",
    interface: "eth0",
    expiresIn: "23 hours, 10 mins"
  },
  {
    id: "lease-3",
    ipAddress: "192.168.1.105",
    macAddress: "B4:F1:DA:C8:E2:10",
    hostname: "iPad-Pro",
    interface: "eth0",
    expiresIn: "2 hours, 15 mins"
  }
]

// Types for Settings & Maintenance
export interface SystemTimeSettings {
  timezone: string
  ntpSync: boolean
  ntpServer: string
}

export interface NetworkServiceStatus {
  id: string
  name: string
  serviceName: string
  status: "running" | "stopped" | "failed"
}

// Initial mockup data for Settings & Maintenance
export const initialSystemTimeSettings: SystemTimeSettings = {
  timezone: "Asia/Bangkok (GMT+7:00)",
  ntpSync: true,
  ntpServer: "pool.ntp.org"
}

export const initialNetworkServices: NetworkServiceStatus[] = [
  {
    id: "srv-1",
    name: "Firewall Engine",
    serviceName: "nftables",
    status: "running"
  },
  {
    id: "srv-2",
    name: "DHCP Server",
    serviceName: "isc-dhcp-server",
    status: "running"
  },
  {
    id: "srv-3",
    name: "Network Core Manager",
    serviceName: "NetworkManager",
    status: "running"
  }
]

export interface DNSRecord {
  id: string
  zoneId: string
  name: string
  type: string
  value: string
  ttl: number
}

export interface DNSZone {
  id: string
  zoneName: string
  forwardTo: string
  allowedIps: string
  isAuthoritative: boolean
  enabled: boolean
  records: DNSRecord[]
}

export const initialDNSZones: DNSZone[] = [
  {
    id: "zone-default-1",
    zoneName: "pigate.local",
    forwardTo: "",
    allowedIps: "any",
    isAuthoritative: true,
    enabled: true,
    records: [
      {
        id: "rec-default-1",
        zoneId: "zone-default-1",
        name: "@",
        type: "A",
        value: "192.168.1.1",
        ttl: 300
      },
      {
        id: "rec-default-2",
        zoneId: "zone-default-1",
        name: "router",
        type: "CNAME",
        value: "pigate.local",
        ttl: 300
      }
    ]
  },
  {
    id: "zone-default-2",
    zoneName: "home.sapray.net",
    forwardTo: "8.8.8.8",
    allowedIps: "any",
    isAuthoritative: false,
    enabled: true,
    records: []
  }
]

// DNS Server listen interfaces (which real LAN interfaces auth-server binds to).
// Kept independent from DHCP Server configuration — sourced from the Interface Service.
export interface DNSServerSettings {
  interfaces: string[]
}

export const initialDNSServerSettings: DNSServerSettings = {
  interfaces: ["eth0"]
}




