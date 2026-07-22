

สร้าง DhcpcdService เพื่อจัดการเรื่อง dhcp โดยเฉพาะ
ลบฟังก์ชั่นจัดการ dhcp ใน real_network.go ให้หมด เพื่อที่จะย้างมาที่ dhcpcdService

การทำงานจะทำแบบนี้

- DhcpcdService จะใช้ NetlinkMonitor ทำงานโดยการดักจับสถานะของ interface เช่น up down เท่านั้น
- จะมีการแบ่งการทำงานเป็นสองแบบ ดังนี้
  - Interface แบบ ethernet
  - Interface แบบ wifi

## Ethernet

1. เมื่อ NetworkMonitor ส่ง Events data มา เช่น

   ```bash
   2026/06/29 14:09:20 [NetlinkMonitor] Received Link event: Index=2, Name=enp0s31f6, Flags=up|broadcast|multicast
   ```
   
2. DhcpcdService จะเช็คประเภทของ interface ว่าไม่ใช้ wifi หรือไม่
3. ถ้าไม่ใช่ wifi จะทำการเช็คการตั้งค่า ip mode ว่าเป็น dhcp หรือไม่ (อาจจะเช็คผ่าน GetDataLayerInterfaceByID ของ InterfaceService ก็ได้)
4. ถ้า ip mode เป็น dhcp ให้ดำเนินต่อ

5. ตรวจสอบ event flags ว่าเป็น up หรือ down
   - down ให้ stop dhcpcd
   - up ให้ start dhcpcd
6. เป็นอันจบขั้นตอน

## Wi-Fi

1. เมื่อ NetworkMonitor ส่ง Events data มา เช่น

   ```bash
   2026/06/29 14:09:20 [NetlinkMonitor] Received Link event: Index=2, Name=wlx0cef1548ff2b, Flags=up|broadcast|multicast
   2026/06/29 14:09:23 [NetlinkMonitor] Received Link event: Index=2, Name=wlx0cef1548ff2b, Flags=up|broadcast|multicast|running
   ```

2. DhcpcdService จะเช็คประเภทของ interface ว่าไม่ใช้ wifi หรือไม่
3. ถ้าใช่ wifi จะทำการเช็คการตั้งค่า ip mode ว่าเป็น dhcp หรือไม่ (อาจจะเช็คผ่าน GetDataLayerInterfaceByID ของ InterfaceService ก็ได้)
4. ถ้า ip mode เป็น dhcp ให้ดำเนินต่อ
5. ตรวจสอบ event flags ว่าเป็น up หรือ down
   - down ให้ stop dhcpcd
   - up ให้ ไปต่อข้อต่อไป
6. เมื่อ up แล้ว ถือว่า interface ทำงานและกำลังพยายามเชื่อมต่อกับ ssid อยู่ เราจะยังไม่ทำอะไรในขั้นตอนนี้
7. เมื่อเชื่อมต่อ ssid แล้ว จะมี flag running ส่งมาด้วย
8. เมื่อมี flag running เราจะทำการสั่ง start dhcpcd
9. เสร็จแล้วก็ปล่อยให้ dhcpcd ทำงานไปเลย

## หมายเหตุ: DHCP Server (dnsmasq) — Domain option (option 15)

เอกสารนี้ครอบคลุมฝั่ง DHCP Client (dhcpcd) เท่านั้น ส่วน DHCP Server (dnsmasq,
`kernel/dhcp_server.go`) แต่ละ scope (`DhcpConfig`) รองรับฟิลด์ `Domain` เพิ่มเติม
(optional) — เมื่อกรอกค่า (เช่น `home.lan`) จะ emit `dhcp-option=<iface>,15,<domain>`
ต่อท้ายบล็อก gateway (opt 3) และ DNS (opt 6) ของ scope นั้น เพื่อแจก DHCP option 15
(domain name) ให้ client ในซับเน็ต ค่าว่าง = ไม่ emit directive (ไม่ regress พฤติกรรมเดิม)
ค่าผ่าน validation แบบ whitelist เดียวกับฟิลด์ DHCP อื่น (`model.ValidateDhcpConfig`)
ก่อนเขียนลงไฟล์ config เสมอ (ดู issue #83)
