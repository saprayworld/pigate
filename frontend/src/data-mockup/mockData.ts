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
