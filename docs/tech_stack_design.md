# PiGate Tech Stack Design & Blueprint

เอกสารฉบับนี้เป็นคู่มือการออกแบบและข้อกำหนดทางเทคนิค (Tech Stack Blueprint) สำหรับการพัฒนาระบบ **PiGate** (Raspberry Pi Firewall/Gateway Controller) โดยมุ่งเน้นไปที่การใช้สถาปัตยกรรมประสิทธิภาพสูง (High-Performance Architecture), ความปลอดภัยระดับแกนหลัก (Kernel-level Security), และลดความเสี่ยงจากการถูกโจมตีด้วยห่วงโซ่อุปทาน (Supply Chain Attack)

---

## 1. System Architecture Overview

สถาปัตยกรรมของ PiGate จะแบ่งออกเป็น 3 Layer หลัก โดยมี **Go (Golang)** ทำหน้าที่เป็นทั้ง API Backend และเซิร์ฟเวอร์เสิร์ฟหน้าเว็บในไฟล์เดี่ยว (Single Binary):

```mermaid
graph TD
    Client[Web Browser / Client UI] -->|HTTPS default, HTTP fallback| GoBinary[Go Executable: Web & API Server]
    GoBinary -->|SQLite Driver: modernc.org/sqlite| DB[(SQLite Database)]
    GoBinary -->|Pure Go Netlink Socket| Kernel[Linux Kernel Space: Netfilter/nftables, Routing, QoS]
    GoBinary -->|wpa_supplicant control socket unixgram| WPA[wpa_supplicant per-interface]
    GoBinary -->|systemd D-Bus| SystemdServices[dhcpcd@iface / systemd-resolved / systemd-hostnamed / systemd-timedated / dnsmasq units]

    subgraph Host [Raspberry Pi 5 Host]
        GoBinary
        DB
        WPA
        SystemdServices
        Kernel
    end
```

ระบบ **ไม่ใช้ NetworkManager/`nmcli`** ในการควบคุมเครือข่าย — Wi-Fi client ควบคุมผ่าน `wpa_supplicant` โดยตรง, DHCP client ฝั่ง WAN ควบคุมผ่าน `dhcpcd@<iface>` (systemd unit) ผ่าน D-Bus, และ DNS ระบบควบคุมผ่าน `systemd-resolved` ผ่าน D-Bus — ดูรายละเอียดกลไกทั้งหมดในหัวข้อที่ 9

---

## 2. Frontend Technology Stack

เพื่อลดภาระประมวลผลบนตัวบอร์ด Raspberry Pi ให้เหลือน้อยที่สุด หน้าบ้านจะถูกออกแบบให้เป็น **Single Page Application (SPA)** เพื่อประมวลผลที่เครื่องผู้ใช้งาน (Client-side Rendering):

* **Core Framework**: **React 19 + Vite**
  * *เหตุผล*: จัดการ UI State ซับซ้อนได้รวดเร็ว มีระบบ Lifecycle ที่นิ่ง และทำงานสอดประสานกับไลบรารี Drag & Drop ล่าสุดได้เต็มประสิทธิภาพ
* **CSS Framework**: **Tailwind CSS v4**
  * *เหตุผล*: ช่วยกำหนด Style ด้วย Utility-first CSS ทำให้เว็บเบา ปรับแต่ง Theme (Dark/Light mode) ผ่านตัวแปรสีเชิงความหมาย (semantic color variables) และสร้างความสวยงามระดับพรีเมียมได้ง่าย
* **Component Library**: **shadcn/ui** (Radix UI Primitives)
  * *เหตุผล*: ต่างจาก UI library ทั่วไป เพราะ shadcn/ui เป็นการ Copy-paste โค้ดคอมโพเนนต์ลงในโปรเจกต์โดยตรง ทำให้เราควบคุมและแก้ไขสไตล์ระดับล่าง (Fine-grain customization) ได้เอง ไม่มีปัญหา Package Bloat หรือส่งผลกระทบต่อขนาด Bundle และมี Accessibility (a11y) สูงมากจากตัว Radix UI ทั้งหน้าบ้านต้องประกอบจากคอมโพเนนต์ใน `components/ui/` เท่านั้น (ดู `docs/rules_of_work.md`)
* **UI Controls & Components**:
  * **@dnd-kit/core** & **@dnd-kit/sortable**: เครื่องมือสำหรับทำ Drag & Drop จัดเรียงกฎความสำคัญของนโยบายไฟร์วอลล์ในหน้า [02-firewall-policies.html](file:///home/sapray/dev/pigate/docs/sketchs/frontend/02-firewall-policies.html)
    * *เหตุผล*: เป็นระบบ Drag & Drop ยุคใหม่ที่ออกแบบมาสำหรับ React โดยเฉพาะ มีขนาดเล็ก รองรับ Touch/Mouse Sensor เต็มรูปแบบ และลื่นไหลกว่าระบบเดิม
  * **Recharts**: ไลบรารีวาดกราฟสถิติ Real-time Traffic ในหน้า [01-dashboard.html](file:///home/sapray/dev/pigate/docs/sketchs/frontend/01-dashboard.html)
  * **Lucide React**: ชุดไอคอนมินิมอลรองรับ SVG
  * **vaul**: Drawer component (ใช้แทน Dialog บางจุดตามดีไซน์การ์ด — ดู `docs/rules_of_work.md`)
  * **sonner**: ระบบ Toast แจ้งเตือนผู้ใช้
  * **zod**: กำหนด schema และตรวจความถูกต้องของฟอร์ม/ข้อมูลฝั่งหน้าบ้านก่อนยิง API
  * **@tanstack/react-table**: จัดการตารางข้อมูล (sort/filter/pagination) ในหน้าที่มีรายการยาว เช่น DHCP leases, Traffic Log
  * **swagger-ui-react**: เรนเดอร์ `docs/openapi.yaml` เป็นหน้า API Docs ในตัวแอป
* **Deployment Pattern**:
  * เมื่อบิวด์โปรเจกต์เสร็จสิ้น (รัน `yarn build`) ไฟล์ HTML, CSS, JS จะถูกนำมาฝังลงในตัวแอปพลิเคชันหลังบ้านผ่านฟีเจอร์ **`go:embed`** ของภาษา Go (ดู `backend/internal/api/embed.go`)

### 2.1 แนวทางการพัฒนาและข้อกำหนดการใช้งานคอมโพเนนต์ (Frontend Component Guidelines)

* สำหรับแนวทางปฏิบัติ ข้อกำหนด และคู่มือการพัฒนาคอมโพเนนต์และการสไตล์หน้าเว็บ สามารถศึกษาเพิ่มเติมได้ที่คู่มือ [rules_of_work.md](file:///home/sapray/dev/pigate/docs/rules_of_work.md)

---

## 3. Backend Technology Stack (Go/Golang)

หลังบ้านพัฒนาด้วยภาษา **Go (Golang)** เพื่อความเร็ว ความเสถียรสูงสุด และป้องกันปัญหา Supply Chain Attack:

* **API Server Engine**: **Go Standard Library (`net/http`)** เท่านั้น ไม่ใช้ web framework ภายนอก (เช่น Fiber/Gin)
  * *เหตุผล*: ลดไลบรารีภายนอกให้เหลือเกือบ 0% ช่วยปิดโอกาสการเกิด Supply Chain Attack ได้อย่างสมบูรณ์ Routing/Middleware (CORS, session auth, rate limit, security headers, body-size cap) ทั้งหมดเขียนขึ้นเองบน `net/http` (`backend/internal/api/`)
* **Input Validation**: พัฒนา Custom validation และจำกัดรูปแบบข้อมูลอินพุตด้วย Regular Expressions และ `net.ParseIP`/`net.ParseCIDR`/`net.ParseMAC` เพื่อป้องกันการโจมตีทางเว็บและ config-file injection เบื้องต้น
* **Database Driver**: **`modernc.org/sqlite`**
  * *เหตุผล*: เป็นตัวเชื่อมต่อ SQLite ที่เขียนด้วย Pure Go (ไม่มี CGO Dependency) ทำให้คอมไพล์โค้ดได้ง่ายและไม่ต้องมี C Compiler ติดตั้งบนคอมพิวเตอร์ที่ใช้พัฒนา

---

## 4. Kernel Integration & Privilege Separation (ความปลอดภัยระดับ OS)

ระบบ Gateway จำเป็นต้องเปลี่ยนการตั้งค่านโยบาย Firewall และสิทธิ์ของพอร์ตเชื่อมต่อ ซึ่งตามปกติจำเป็นต้องใช้สิทธิ์สูงสุด (Root) เพื่อความปลอดภัยสูงสุด ระบบจึงออกแบบแนวทางการจัดการสิทธิ์ดังนี้:

### 4.1 Direct Socket Interaction (หลีกเลี่ยง Shell Commands)
การเรียกใช้คำสั่งผ่านเชลล์มีความเสี่ยงต่อช่องโหว่ Command Injection หากกรองค่าอินพุตไม่รัดกุม ระบบ PiGate ในส่วน Go Backend จึงสื่อสารกับเคอร์เนลและบริการของ OS โดยตรง ไม่ผ่าน `exec.Command` สำหรับงานที่มีทางเลือกแบบ Netlink/D-Bus:
* **Firewall (nftables)**: ใช้ไลบรารี [google/nftables](https://github.com/google/nftables) (Pure Go) ในการอ่าน เขียน และแก้ไขนโยบายของ Netfilter โดยตรงผ่านระดับ Netlink Socket
* **Network & Routing & QoS**: ใช้ [vishvananda/netlink](https://github.com/vishvananda/netlink) ในการจัดการสถานะอินเทอร์เฟซ, เพิ่ม/ลบไอพีแอดเดรส, ตารางเส้นทาง (Routing Table) และคิวส่งข้อมูล (`tc` HTB/IFB สำหรับ QoS)
* **Forward Traffic Log**: ใช้ [florianl/go-nflog](https://github.com/florianl/go-nflog) รับ log ของ `forward` chain ผ่าน NFLOG multicast group แทนการ tail syslog
* **Wi-Fi client**: เขียนไฟล์คอนฟิกของ `wpa_supplicant` แบบ atomic ต่ออินเทอร์เฟซ (`/etc/wpa_supplicant/wpa_supplicant-<iface>.conf`, permission 0600) แล้วสั่งงานผ่าน Unix control socket ของ `wpa_supplicant` เอง (`unixgram`, ไม่ใช่ subprocess) — รายละเอียดเต็มดู `docs/wifi_wpa_working_instruction.md`
* **OS Services อื่นๆ** (DHCP client ฝั่ง WAN, DNS ระบบ, DHCP server, DNS server, hostname, เวลาระบบ): เชื่อมต่อผ่าน D-Bus API ของ systemd โดยตรง (ดูหัวข้อที่ 9)

### 4.2 Linux Capabilities (ลดขอบเขตการยึดครองระบบ)
ตัวแอปพลิเคชัน Go จะไม่ถูกรันด้วยสิทธิ์ผู้ใช้ `root` โดยตรง แต่จะถูกกำหนดเป็นสิทธิ์ผู้ใช้ระบบเฉพาะ (`pigate`) และนำคุณสมบัติ **Linux Capabilities** ไปผูกไว้ที่ตัวไฟล์ Executable เพื่อให้รันงานที่เกี่ยวข้องกับเครือข่ายได้เท่านั้น:
```bash
sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
```
* **`cap_net_admin`**: สิทธิ์ในการตั้งค่า Network Interface, IP, Routes และ Firewall Tables (`nftables`)
* **`cap_net_raw`**: สิทธิ์ในการสร้าง Raw Sockets (จำเป็นสำหรับการรันคำสั่ง Ping หรือจับแพ็กเก็ต)
* *ข้อดี*: หากแอปพลิเคชันหน้าเว็บมีช่องโหว่ RCE แฮกเกอร์ก็จะไม่สามารถเข้ามาเขียนทับไฟล์ระบบ หรือลบไฟล์ส่วนอื่นๆ ใน OS ได้เนื่องจากรันภายใต้สิทธิ์ผู้ใช้จำกัดทั่วไป งานที่ยังต้องอาศัยสิทธิ์เพิ่ม (เช่น สั่ง `dhcpcd@<iface>`/`wpa_supplicant`/`systemd-resolved` ผ่าน D-Bus) ถูกจำกัดขอบเขตเพิ่มอีกชั้นด้วย Polkit rule และ sudoers entry แบบเจาะจงรายคำสั่ง+อาร์กิวเมนต์ที่ `install.sh` ติดตั้งให้

### 4.3 Default Firewall Rules & Auditing Architecture (การตั้งค่ากฎไฟร์วอลล์หลักและการตรวจสอบบัญชี)
ระบบ PiGate ได้กำหนดโครงสร้างของกฎไฟร์วอลล์พื้นฐาน (Default rules) สำหรับปกป้องอินเทอร์เฟซขาเข้า (INPUT Chain) โดยจัดวางแบบ **Declarative nftables format** และจัดลำดับการคัดกรองร่วมกับระบบ Auditing และ Docker Compatibility ดังต่อไปนี้:

1. **โครงสร้างการประมวลผล (Processing Sequence)**:
   * **ส่วนที่ 1: กฎความปลอดภัยเบื้องต้น (Drop & Sanity Checks)**: บล็อกแพ็กเก็ตชำรุด (INVALID), Loopback whitelist, ICMP diagnostics, บล็อกพอร์ต Samba/SMB, บล็อก rogue DHCP, บล็อก Broadcast, คัดกรอง IP Spoofing ผ่าน custom chain `pigate-not-local` และยอมรับ mDNS/SSDP
   * **ส่วนที่ 2: จุดเริ่มต้นการตรวจสอบสิทธิ์ (Audit Log)**: พ่นข้อมูลแพ็กเก็ตที่ผ่านการกรองเบื้องต้นลง syslog ด้วย Prefix `[PiGate] INP AUDIT : ` เพื่อเป็นหลักฐานว่ามีข้อมูลผ่านเข้ามาสู่ชั้นตัดสินสิทธิ์
   * **ส่วนที่ 3: กฎไดนามิกและการอนุญาต (Dynamic Accept Rules)**: ยอมรับและสตรีมล็อก (`[PiGate] INP ACCEPT: `) สำหรับพอร์ตบริการ/IP ที่ผ่านเงื่อนไขจาก Database (เช่น HTTP, HTTPS, SSH, PING) และเชื่อมโยงกับการตั้งค่า Docker Compatibility (เช่น การยอมรับ `docker0` และ `br-*` อัตโนมัติเมื่อเปิดแฟล็ก)
   * **ส่วนที่ 4: แพ็กเก็ตที่เหลือทั้งหมด (Drop Log)**: พ่นล็อกลง syslog ด้วย Prefix `[PiGate] INP DROP  : ` เพื่อความสะดวกในการติดตามเหตุการณ์ว่าแพ็กเก็ตชิ้นใดถูกบล็อกโดย Default Policy (`DROP`)

2. **โครงสร้างโมเดล nftables ตัวอย่าง**:
   * การประยุกต์กฎไฟร์วอลล์โดยใช้ Netlink (ผ่าน `google/nftables` ใน Go) จะทำงานแบบ Transactional API โครงสร้างโค้ดที่สร้างใน Kernel จะสอดคล้องตามตัวอย่างด้านล่าง:

```nftables
table inet pigate {
    chain pigate-not-local {
        fib daddr type local return
        fib daddr type multicast return
        fib daddr type broadcast return
        limit rate 3/minute burst 10 packets log prefix "[PiGate]  INP DROP  : "
        drop
    }

    chain input {
        type filter hook input priority filter; policy drop;

        # --- Section 1: Sanity & Drop Checks ---
        ct state established,related accept
        ct state invalid drop
        iifname "lo" accept
        icmp type { destination-unreachable, time-exceeded, parameter-problem, echo-request } accept
        udp dport { 137, 138, 67, 68 } drop
        tcp dport { 139, 445 } drop
        fib daddr type broadcast drop
        jump pigate-not-local
        ip daddr 224.0.0.251 udp dport 5353 accept
        ip daddr 239.255.255.250 udp dport 1900 accept

        # --- Section 2: Audit Point ---
        log prefix "[PiGate] INP AUDIT : "

        # --- Section 3: Dynamic Accepts (From DB / Docker Compat) ---
        # [Docker Compat]
        # iifname "docker0" log prefix "[PiGate] INP ACCEPT: " accept
        # iifname "br-*" log prefix "[PiGate] INP ACCEPT: " accept
        
        # [Dynamic from DB AdminAccess config]
        # iifname "eth0" tcp dport { 22, 2479 } log prefix "[PiGate] INP ACCEPT: " accept

        # --- Section 4: Final Drop Log ---
        log prefix "[PiGate] INP DROP  : "
    }
}
```

3. **การบันทึกสถิติกฎไฟร์วอลล์ (Firewall Rule Counters)**:
   * **กลไกการทำงาน**: ทุกกฎไฟร์วอลล์ไดนามิกใน `forward` และ `input` chain จะถูกแนบตัวจับคู่สถิติ (`counter`) ในเคอร์เนล โดย Linux Kernel จะบันทึกจำนวนครั้งที่เงื่อนไขกฎสอดคล้อง (`packets` หรือ hit count) และปริมาณขนาดข้อมูลของแต่ละแพ็กเก็ต (`bytes`) โดยอัตโนมัติ
   * **การดึงข้อมูลสด (Real-time Fetching)**: Go Backend จะเรียกอ่านค่าสถิติเหล่านี้ผ่านทาง Netlink API (`c.GetRules()`) แล้วส่งค่าสด (Live telemetry) ไปยังหน้าบ้านผ่าน REST API `/api/policies` เพื่อลดภาระการเขียนข้อมูลปริมาณมากลงหน่วยความจำถาวร SQLite บน SD Card ช่วยยืดอายุการใช้งาน MicroSD Card ของ Raspberry Pi 5 (สอดคล้องกับแนวทางในหัวข้อที่ 8)
   * **การแสดงผลบนหน้าต่างควบคุม (UI Visualization)**: หน้าจอระบบจัดการกฎไฟร์วอลล์ฝั่งหน้าบ้านจะแสดงผล Hit Count และ Traffic Volume ท้ายแถวของแต่ละนโยบาย เพื่อให้ผู้ดูแลระบบมองเห็นประสิทธิภาพและความจำเป็นของกฎแต่ละข้อได้ทันที

4. **การจัดการ NAT (Network Address Translation / IP Masquerade) บนขา WAN**:
   * **กลไกการทำงาน**: เพื่อให้เครื่องภายในวง LAN สามารถเชื่อมต่อออกสู่อินเทอร์เน็ตภายนอกได้ ระบบจะทำการแชร์อินเทอร์เน็ตผ่านการทำ IP Masquerade บนอินเทอร์เฟซฝั่ง WAN
   * **โครงสร้าง nftables**: โครงสร้าง NAT จะแยกเป็นตารางเฉพาะ (เช่น `pigate_nat` ของ Family `ip`) และจัดการผ่าน `postrouting` hook ดังตัวอย่าง:
```nftables
table ip pigate_nat {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        oifname "wlan0" masquerade  # ทำ Masquerade เฉพาะข้อมูลขาออกผ่านการ์ดที่เป็น WAN
    }
}
```
   * **การประยุกต์แบบไดนามิก (Dynamic Binding)**: Go Backend จะตรวจสอบรายชื่อการ์ดเครือข่ายจากฐานข้อมูลที่มีหน้าที่เป็น WAN (`Role = WAN`) และสร้างกฎ Masquerade สำหรับอินเทอร์เฟซเหล่านั้นโดยอัตโนมัติเมื่อสั่ง Apply Settings

5. **Port Forwarding (DNAT)**:
   * **กลไกการทำงาน**: การส่งต่อพอร์ตจาก WAN เข้าสู่โฮสต์ภายใน LAN (Destination NAT) สร้างเป็น chain แยก `prerouting` ที่ทำงาน**ก่อน**การตัดสินใจ routing เพื่อให้แพ็กเก็ตถูกส่งต่อไปยัง IP ภายในที่ถูกต้อง — สร้างเป็น nftables expression ของ `google/nftables` โดยตรง ไม่ใช่ shell string จึงไม่มีช่องให้ inject
   * ทุกค่า (interface, protocol, IP ปลายทาง, ช่วงพอร์ต) ผ่านการ validate ที่ชั้น service/repository ก่อน persist ลง DB และก่อน apply ลง kernel เสมอ

---

## 5. Security & Protection Against Supply Chain Attack

ระบบ Go ได้กำหนดแนวทางความปลอดภัยของโค้ดต้นทางและการนำไปใช้ไว้ดังนี้:

1. **Strict Dependency Pinning & Verification**:
   * การใช้ `go.sum` เพื่อเก็บ Cryptographic Hash สำหรับรับประกันว่าไม่มีโมดูลตัวใดถูกแก้ไขโค้ดระหว่างทาง
   * การประเมินและคัดกรอง Dependency ภายนอก โดยเลือกใช้เฉพาะระดับ Core SDK หรือคลังเก็บสถิติที่มีผู้ดูแลเป็นทางการ (เช่น `golang.org/x/...` หรือ `github.com/google/...`)
2. **Static Linking**:
   * การคอมไพล์โค้ดออกเป็นไฟล์ Executable ไฟล์เดียว ช่วยลดความซับซ้อนในการติดตั้งโมดูลย่อยบนระบบลินุกซ์ และทำให้มั่นใจได้ว่าโค้ดที่รันอยู่เบื้องหลังเป็นเวอร์ชันเดียวกันกับที่ผ่านการตรวจสอบ
3. **No Dynamic Code Evaluation**:
   * ภาษา Go ไม่มีคำสั่งประเมินผลโค้ดขณะรัน (เช่น `eval()` ใน JS/Python) ทำให้ปิดช่องโหว่การโจมตีประเภท Dynamic Code Injection ได้โดยปริยาย

---

## 6. Authentication, Session & Access Control

ระบบความปลอดภัยในการเข้าถึง API สำหรับผู้ดูแลระบบ:
* **Authentication**: ล็อกอินตรวจสอบรหัสผ่านด้วย **bcrypt** (cost ≥ 10) การเข้ารหัสไฟล์ backup ใช้อัลกอริทึมคนละชุด คือ **AES-256-GCM + Argon2id** (ดูหัวข้อที่ 9) — ทั้งสองกรณีไม่ใช้ JWT
* **Session Management**: หลังล็อกอินสำเร็จ server สร้าง session token แบบสุ่มจาก `crypto/rand` เก็บไว้ใน in-memory map ฝั่ง backend เท่านั้น (ไม่ persist ลง SQLite) แล้วส่งกลับผ่าน **`Set-Cookie`** ที่ตั้งค่า `HttpOnly`, `SameSite=Strict` และ `Secure` (ตั้งตาม `r.TLS` ของแต่ละ request — เป็นจริงเมื่อรันผ่าน HTTPS) เป็นช่องทางเดียว ไม่ส่ง token ปนไปใน JSON response body และฝั่งหน้าบ้านไม่เก็บ token ใน `localStorage`
  * *เหตุผล*: ป้องกันการโจมตีประเภท Cross-Site Scripting (XSS) ไม่ให้เข้าถึงโทเค็นเพื่อนำไปทำ Session Hijacking ได้ และป้องกัน CSRF ด้วย `SameSite=Strict`
  * Session มีทั้ง sliding idle TTL (ต่ออายุเมื่อมี activity) และเพดานอายุสัมบูรณ์ (absolute cap) รวมถึงจำกัดจำนวน session ต่อผู้ใช้ พร้อม sweeper กวาดทิ้ง session ที่หมดอายุเป็นระยะ
* **Rate Limiting**: เพิ่มระบบทำ Rate Limiter (Token Bucket ต่อ IP) บน API ล็อกอิน เพื่อป้องกันการถูกเดารหัสผ่านแบบสุ่ม (Brute-Force Attacks)
* **RBAC**: บทบาทผู้ใช้ (เช่น `super_admin`, `admin_readonly`) ตรวจสอบแบบ fail-closed ที่ middleware ทุก route ที่แก้ไขข้อมูลหรือคืนความลับต้องผ่านการตรวจสอบสิทธิ์เสมอ

---

## 7. Real-Time Data Streaming (Server-Sent Events)

สำหรับการอัปเดตสถิติ Real-time Performance และทราฟฟิก WAN ในหน้า Dashboard:
* **Technology**: **Server-Sent Events (SSE)**
  * *เหตุผล*: SSE เป็นโปรโตคอลการส่งข้อมูลแบบทิศทางเดียว (Server-to-Client) ที่รันผ่าน HTTP โปรโตคอลมาตรฐาน ทำให้เขียนโค้ดฝั่ง Go Backend ได้ง่ายโดยใช้เพียง `http.ResponseWriter` มาตรฐานโดยไม่ต้องลงไลบรารีเสริม และกินทรัพยากรระบบน้อยกว่าการใช้ WebSockets (ซึ่งต้องทำ Handshake และอัปเกรดโปรโตคอลแยกต่างหาก)

---

## 8. Logging & SD Card Preservation (ถนอมอายุการใช้งาน MicroSD Card)

บอร์ด Raspberry Pi มักใช้ MicroSD Card ในการเก็บระบบปฏิบัติการ ซึ่งมีจำนวนรอบการเขียน (Write Cycles) ที่จำกัด การบันทึกล็อกปริมาณมากอาจทำให้การ์ดชำรุดเสียหายเร็วขึ้น:
* **Log Storage**: หลีกเลี่ยงการเขียนสถิติล็อกของ Firewall (nftables Block Logs) และ session ลงในฐานข้อมูล SQLite บนดิสก์อย่างต่อเนื่อง
* **Solution**:
  * บันทึกข้อมูลประวัติ Log ล่าสุดไว้ใน **In-Memory Circular Buffer (Ring Buffer)** บนแรมของ Go API (`backend/internal/logs/ringbuffer.go`) — ขนาดคงที่ ไม่ persist ลงดิสก์
  * ใช้การอ่าน/ดึงข้อมูลสตรีมโดยตรงจาก **Systemd Journald** หรือเก็บไฟล์ล็อกไว้ใน `/run/` หรือ `/tmp/` (ซึ่งเป็น `tmpfs` หรือแรมเสมือนใน Linux) เพื่อลดภาระการเขียนข้อมูลลงหน่วยความจำถาวร
  * SQLite (`db/`) เก็บเฉพาะ**การตั้งค่า** (source of truth ของ config) ส่วนสถานะที่เปลี่ยนบ่อย (firewall hit counter, DHCP lease สด) จะอ่านสดจาก kernel ทุกครั้งแทนการ cache ลงดิสก์

---

## 9. Network & OS Service Control Mechanism (wpa_supplicant / D-Bus)

การควบคุม LAN/Wi-Fi/DNS/เวลาใน PiGate **ไม่ผ่าน NetworkManager** — แต่ละบริการของ OS ถูกควบคุมด้วยกลไกที่เหมาะสมกับตัวมันเองโดยตรง ไม่มีจุดใดเรียก `nmcli`/`nmtui` หรือ parse stdout ของคอมมานด์ไลน์:

* **Wi-Fi client (`internal/kernel/real_network.go` + `wpa.go`)**: เขียนไฟล์คอนฟิกต่ออินเทอร์เฟซ `/etc/wpa_supplicant/wpa_supplicant-<iface>.conf` แบบ atomic (เขียนไฟล์ชั่วคราวแล้ว `rename`) permission 0600 พร้อม sanitize อินพุต (ตัด `\n`, `\r`, `"` ก่อนฝังใน config) จากนั้นสั่งให้ `wpa_supplicant` โหลดค่าใหม่ผ่าน Unix control socket (`unixgram`) ของมันเอง — ดูรายละเอียดเต็มที่ `docs/wifi_wpa_working_instruction.md`
* **DHCP client ฝั่ง WAN (`internal/kernel/dhcpcd.go`)**: แต่ละอินเทอร์เฟซมี systemd unit ของตัวเอง (`dhcpcd@<iface>.service`) — Go backend สั่ง start/stop/restart ผ่าน D-Bus ของ `org.freedesktop.systemd1` เท่านั้น
* **DNS ระบบ (`internal/kernel/dns.go`)**: อ่าน/ตั้งค่า DNS ต่อ link และ DNS ทั้งระบบผ่าน D-Bus ของ `systemd-resolved` (`org.freedesktop.resolve1`) และไฟล์ drop-in `/etc/systemd/resolved.conf.d/`
* **DHCP server (`internal/kernel/dhcp_server.go`)**: สร้างไฟล์คอนฟิก `dnsmasq` จากค่าใน DB ตรวจ syntax ด้วย `dnsmasq --test` ก่อน apply แล้ว restart service ผ่าน D-Bus; อ่าน active lease จากไฟล์ lease ของ dnsmasq
* **DNS server / local zones (`internal/kernel/dns_server.go`)**: สร้างไฟล์ zone ของ `dnsmasq` จาก DB ในลักษณะเดียวกัน มี input validation ครบทั้งชั้น handler, backup import และ generation-time
* **Hostname (`internal/kernel/real_hostname.go`)**: อ่าน/ตั้งชื่อโฮสต์ผ่าน D-Bus ของ `systemd-hostnamed` (`org.freedesktop.hostname1`) ซึ่งจัดการเขียน `/etc/hostname` ให้เองแบบ atomic
* **เวลาระบบ / NTP (`internal/kernel/real_timedate.go`)**: อ่าน/ตั้งเขตเวลา, เปิด-ปิด NTP sync ผ่าน D-Bus ของ `systemd-timedated` (`org.freedesktop.timedate1`)
* **QoS (`internal/kernel/real_qos.go`)**: จำกัดแบนด์วิดท์ด้วย Linux Traffic Control ผ่าน `vishvananda/netlink` โดยตรง — สร้าง HTB (Hierarchical Token Bucket) qdisc/class สำหรับ egress และใช้ IFB (Intermediate Functional Block) redirect เพื่อจำกัด ingress
* **Forward Traffic Log (`internal/kernel/real_traffic_log.go`)**: subscribe NFLOG multicast group ที่ nftables ส่ง log ของ `forward` chain มาให้ ผ่านไลบรารี `florianl/go-nflog` (Netlink ล้วน ไม่ tail syslog)

*เหตุผลรวม*: ได้ผลลัพธ์การทำงานที่แม่นยำกว่าการจับข้อความจาก stdout ของคอมมานด์ไลน์ ปลอดภัยจากการโจมตี command injection โดยดีไซน์ และทำงานได้เร็วกว่า

---

## 10. Backend Layer Structure

ออกแบบและเขียนระบบโดยใช้ Layer system เพื่อแยกการทำงานในแต่ละส่วนออกจากกันอย่างเคร่งครัด (`backend/internal/`):

* **`api/`** — HTTP handler, routing, middleware (CORS/auth/rate-limit/security headers); คุยกับ `service/` เท่านั้น ไม่แตะ kernel/DB ตรง
* **`service/`** — business logic (`firewall.go`, `routing.go`, `interface.go`, `dns.go`, `dhcp_server.go`, `qos.go`, `user.go`, `backup.go` ฯลฯ) อ่าน/เขียนผ่าน repository และสั่งงาน kernel layer ผ่าน interface
* **`kernel/`** — เลเยอร์เดียวที่คุยกับ OS ได้ แต่ละ subsystem ประกาศเป็น interface ใน `interfaces.go` (เช่น `FirewallManager`, `NetworkManager`, `RoutingManager`, `DhcpManager`, `DNSManager`, `QosManager`, `DNSServerManager`, `HostnameManager`, `TimeManager`) และมีสอง implementation เสมอ: `real_*.go` (Netlink/D-Bus/`google/nftables`/`vishvananda/netlink` สำหรับใช้งานจริง) กับ `mock.go` (จำลองในหน่วยความจำ ปลอดภัยสำหรับ dev บนเครื่องทั่วไป) — เพิ่มความสามารถใหม่ในเลเยอร์นี้ต้องเพิ่มทั้งสองฝั่งเสมอ
* **`db/`** — SQLite (ผ่าน `modernc.org/sqlite`) คือ source of truth ของ config ทั้งหมด
* **`model/`** — struct/DTO ที่ใช้ร่วมกันทุกเลเยอร์
* **`logs/`** — ring buffer ในหน่วยความจำสำหรับ log แบบ real-time (ดูหัวข้อที่ 8)
