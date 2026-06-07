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
  source: string[]
  destination: string[]
  service: string[]
  action: "ACCEPT" | "DROP"
  log: boolean
  status: boolean // true = Enabled, false = Disabled
}

// Initial mockup rules for Firewall Policy
export const initialPolicyRules: PolicyRule[] = [
  {
    id: "rule-1",
    name: "Allow-DNS-Out",
    source: ["LAN_Network", "192.168.1.0/24"],
    destination: ["8.8.8.8", "1.1.1.1"],
    service: ["UDP (53)", "TCP (53)"],
    action: "ACCEPT",
    log: false,
    status: true
  },
  {
    id: "rule-2",
    name: "Allow-Web-Out",
    source: ["LAN_Network"],
    destination: ["ALL (Internet)"],
    service: ["HTTP (80)", "HTTPS (443)"],
    action: "ACCEPT",
    log: false,
    status: true
  },
  {
    id: "rule-3",
    name: "Block-BitTorrent",
    source: ["LAN_Network"],
    destination: ["ALL"],
    service: ["BitTorrent_Ports"],
    action: "DROP",
    log: true,
    status: true
  },
  {
    id: "rule-4",
    name: "Block-Malicious-IPs",
    source: ["ALL"],
    destination: ["Malicious_IP_List"],
    service: ["ALL"],
    action: "DROP",
    log: true,
    status: true
  },
  {
    id: "rule-5",
    name: "Allow-SSH-Admin",
    source: ["Admin_Host", "192.168.1.100"],
    destination: ["PiGate_Host"],
    service: ["SSH (22)"],
    action: "ACCEPT",
    log: true,
    status: true
  }
]

// Types for Address Objects
export interface AddressObject {
  id: string
  name: string
  type: "subnet" | "range" | "fqdn"
  value: string
  refPolicies: string[]
}

// Initial mockup data for Address Objects
export const initialAddressObjects: AddressObject[] = [
  {
    id: "addr-1",
    name: "LAN_Network",
    type: "subnet",
    value: "192.168.1.0/24",
    refPolicies: ["Allow-DNS-Out", "Allow-Web-Out", "Block-BitTorrent"]
  },
  {
    id: "addr-2",
    name: "Admin_PC",
    type: "subnet",
    value: "192.168.1.10/32",
    refPolicies: ["Allow-SSH-Admin"]
  },
  {
    id: "addr-3",
    name: "DHCP_Pool_Zone",
    type: "range",
    value: "192.168.1.100 - 192.168.1.200",
    refPolicies: []
  },
  {
    id: "addr-4",
    name: "Update_Server",
    type: "fqdn",
    value: "pigate-update.com",
    refPolicies: ["System-Update"]
  },
  {
    id: "addr-5",
    name: "Malicious_IP_List",
    type: "subnet",
    value: "198.51.100.0/22",
    refPolicies: ["Block-Malicious-IPs"]
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
    name: "HTTP",
    protocol: "TCP",
    port: "80",
    type: "system",
    refPolicies: ["Allow-Web-Out"]
  },
  {
    id: "svc-2",
    name: "HTTPS",
    protocol: "TCP",
    port: "443",
    type: "system",
    refPolicies: ["Allow-Web-Out"]
  },
  {
    id: "svc-3",
    name: "SSH",
    protocol: "TCP",
    port: "22",
    type: "system",
    refPolicies: ["Allow-SSH-Admin"]
  },
  {
    id: "svc-4",
    name: "DNS",
    protocol: "UDP",
    port: "53",
    type: "system",
    refPolicies: ["Allow-DNS-Out"]
  },
  {
    id: "svc-5",
    name: "My_Minecraft_Server",
    protocol: "TCP/UDP",
    port: "25565",
    type: "custom",
    refPolicies: []
  },
  {
    id: "svc-6",
    name: "Web_Testing_Pool",
    protocol: "TCP",
    port: "8080-8085",
    type: "custom",
    refPolicies: []
  },
  {
    id: "svc-7",
    name: "BitTorrent_Ports",
    protocol: "TCP/UDP",
    port: "6881-6889",
    type: "custom",
    refPolicies: ["Block-BitTorrent"]
  },
  {
    id: "svc-8",
    name: "Ping_ICMP",
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
  type: InterfaceType
  addressingMode: AddressingMode
  ip: string                  // e.g. "192.168.1.1"
  netmask: string             // e.g. "24"
  gateway: string             // e.g. "192.168.1.254" (used for static)
  dns1: string
  dns2: string
  macAddress: string
  adminAccess: AdminAccess[]
  status: "up" | "down"
  speed: string               // e.g. "1000 Mbps", "72 Mbps"
  // Wi-Fi specific
  connectedSSID?: string
  wifiPassword?: string       // masked
  wifiSecurity?: string       // e.g. "WPA2-PSK"
}

// Initial mockup data for Network Interfaces
export const initialNetworkInterfaces: NetworkInterface[] = [
  {
    id: "iface-1",
    name: "eth0",
    alias: "LAN_Internal",
    type: "ethernet",
    addressingMode: "static",
    ip: "192.168.1.1",
    netmask: "24",
    gateway: "",
    dns1: "",
    dns2: "",
    macAddress: "DC:A6:32:AA:BB:C1",
    adminAccess: ["PING", "HTTP", "SSH"],
    status: "up",
    speed: "1000 Mbps"
  },
  {
    id: "iface-2",
    name: "wlan0",
    alias: "WAN_WiFi",
    type: "wireless",
    addressingMode: "dhcp",
    ip: "10.0.0.45",
    netmask: "24",
    gateway: "10.0.0.1",
    dns1: "8.8.8.8",
    dns2: "1.1.1.1",
    macAddress: "DC:A6:32:AA:BB:C2",
    adminAccess: ["PING"],
    status: "up",
    speed: "72 Mbps",
    connectedSSID: "MyHome_5G",
    wifiPassword: "••••••••",
    wifiSecurity: "WPA2-PSK"
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

