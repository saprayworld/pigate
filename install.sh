#!/bin/bash
# =============================================================================
# PiGate Installation Script
# Based on: docs/setup_guide.md
# =============================================================================

set -euo pipefail

# --- Color output ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info()    { echo -e "${BLUE}[INFO]${NC}  $*"; }
log_ok()      { echo -e "${GREEN}[OK]${NC}    $*"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $*"; }

# --- ตรวจสอบว่ารันด้วย sudo ---
if [[ $EUID -ne 0 ]]; then
    log_error "กรุณารัน script นี้ด้วย sudo: sudo bash install.sh"
    exit 1
fi

# --- ตรวจสอบไฟล์ binary ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_SRC="${SCRIPT_DIR}/pigate"

if [[ ! -f "${BINARY_SRC}" ]]; then
    log_error "ไม่พบไฟล์ binary: ${BINARY_SRC}"
    log_error "กรุณา build โปรเจกต์ก่อนด้วย: bash build.sh"
    exit 1
fi

echo ""
echo -e "${BLUE}=============================================${NC}"
echo -e "${BLUE}       PiGate Installation Script          ${NC}"
echo -e "${BLUE}=============================================${NC}"
echo ""

# =============================================================================
# ตรวจสอบว่าเป็นการ Update/Reinstall หรือติดตั้งใหม่
# =============================================================================
IS_UPDATE=false
BINARY_INSTALLED="/usr/local/bin/pigate"
SERVICE_NAME="pigate.service"
SERVICE_WAS_RUNNING=false

if [[ -f "${BINARY_INSTALLED}" ]] || systemctl list-unit-files "${SERVICE_NAME}" &>/dev/null && systemctl is-enabled "${SERVICE_NAME}" &>/dev/null 2>&1; then
    IS_UPDATE=true
fi

if [[ "${IS_UPDATE}" == true ]]; then
    echo -e "${YELLOW}=============================================${NC}"
    echo -e "${YELLOW}  ⚠  พบการติดตั้ง PiGate อยู่แล้วในระบบ!  ${NC}"
    echo -e "${YELLOW}=============================================${NC}"
    echo ""

    # แสดงสถานะปัจจุบัน
    if [[ -f "${BINARY_INSTALLED}" ]]; then
        log_info "Binary:  ${BINARY_INSTALLED} (พบไฟล์)"
    fi

    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        log_warn "Service: ${SERVICE_NAME} กำลังทำงานอยู่ (active)"
        SERVICE_WAS_RUNNING=true
    elif systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null; then
        log_info "Service: ${SERVICE_NAME} ถูก enable แต่ไม่ได้ทำงาน"
    fi

    echo ""
    echo -e "${YELLOW}Script จะดำเนินการดังนี้:${NC}"
    echo -e "  1. หยุด service pigate (ถ้ากำลังรันอยู่)"
    echo -e "  2. อัปเดต binary และไฟล์ config ทั้งหมด"
    echo -e "  3. เริ่ม service ใหม่ (ถ้าเคยรันอยู่ก่อน)"
    echo ""
    read -r -p "$(echo -e "${YELLOW}ต้องการดำเนิน Update/Reinstall ต่อหรือไม่? [y/N]: ${NC}")" CONFIRM

    if [[ ! "${CONFIRM}" =~ ^[Yy]$ ]]; then
        log_warn "ยกเลิกการติดตั้ง"
        exit 0
    fi

    echo ""
    log_info "เริ่มต้น Update/Reinstall PiGate..."

    # หยุด service ก่อนอัปเดต
    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        log_info "กำลังหยุด ${SERVICE_NAME}..."
        systemctl stop "${SERVICE_NAME}"
        log_ok "หยุด ${SERVICE_NAME} สำเร็จ"
    fi
else
    log_info "ไม่พบการติดตั้งเดิม — เริ่มติดตั้งใหม่..."
fi

echo ""

# =============================================================================
# STEP 1: สร้าง system user สำหรับ pigate
# =============================================================================
log_info "STEP 1: สร้าง system user 'pigate'..."

if id "pigate" &>/dev/null; then
    log_warn "User 'pigate' มีอยู่แล้ว — ข้ามขั้นตอนนี้"
else
    useradd -r -s /usr/sbin/nologin pigate
    log_ok "สร้าง user 'pigate' สำเร็จ"
fi

# เพิ่ม pigate เข้ากลุ่ม netdev
usermod -aG netdev pigate
log_ok "เพิ่ม 'pigate' เข้ากลุ่ม 'netdev' สำเร็จ"

# =============================================================================
# STEP 2: ตั้งค่า ACL สำหรับ wpa_supplicant
# =============================================================================
log_info "STEP 2: ตั้งค่า ACL สำหรับ /etc/wpa_supplicant..."

if ! command -v setfacl &>/dev/null; then
    log_warn "ไม่พบคำสั่ง setfacl — กำลังติดตั้ง acl..."
    apt-get install -y acl
fi

mkdir -p /etc/wpa_supplicant
setfacl -m u:pigate:rwx /etc/wpa_supplicant
setfacl -d -m u:pigate:rwx /etc/wpa_supplicant
log_ok "ตั้งค่า ACL สำหรับ /etc/wpa_supplicant สำเร็จ"

# =============================================================================
# STEP 2.1: ติดตั้งและตั้งค่า ACL สำหรับ dnsmasq
# =============================================================================
log_info "STEP 2.1: ติดตั้งและตั้งค่า ACL สำหรับ dnsmasq..."

if ! command -v dnsmasq &>/dev/null; then
    log_info "ไม่พบ dnsmasq — กำลังติดตั้ง dnsmasq..."
    apt-get update && apt-get install -y dnsmasq
fi

# สร้าง directory สำหรับคอนฟิกของ dnsmasq (ถ้ายังไม่มี)
mkdir -p /etc/dnsmasq.d

# ตั้งค่า ACL เพื่ออนุญาตให้ user 'pigate' สามารถเขียนไฟล์คอนฟิกได้
setfacl -m u:pigate:rwx /etc/dnsmasq.d
setfacl -d -m u:pigate:rwx /etc/dnsmasq.d
log_ok "ตั้งค่า ACL สำหรับ /etc/dnsmasq.d สำเร็จ"

# ปิดกลไก resolvconf hook (start-resolvconf) ของ dnsmasq package บน Debian/Ubuntu
# เหตุผล: package รัน dnsmasq ด้วย `-r /run/dnsmasq/resolv.conf` ซึ่งถูกเติมโดย hook
# start-resolvconf — hook นี้ fail บนเครื่องที่ไม่มี resolvconf/มี interface loopback
# ("Link lo is loopback device") ทำให้ dnsmasq ไม่มี upstream และเข้าสู่โหมด REFUSED
# ตั้ง IGNORE_RESOLVCONF=yes เพื่อตัด dependency นี้ — PiGate เขียน upstream (server=)
# ลงใน pigate-dns.conf เองแทน ค่านี้เป็น env var ของ init/systemd-helper (ไม่ใช่ dnsmasq
# directive) จึงห้ามใส่ปนใน pigate-*.conf และมีผลเฉพาะตอน service (re)start
# หมายเหตุ: ไม่แตะ systemd-resolved — เป็นคนละกลไก ยังต้องใช้กับ System DNS
if [ -f /etc/default/dnsmasq ]; then
    if grep -q "^IGNORE_RESOLVCONF=" /etc/default/dnsmasq; then
        sed -i 's/^IGNORE_RESOLVCONF=.*/IGNORE_RESOLVCONF=yes/' /etc/default/dnsmasq
    elif grep -q "^#IGNORE_RESOLVCONF=" /etc/default/dnsmasq; then
        sed -i 's/^#IGNORE_RESOLVCONF=.*/IGNORE_RESOLVCONF=yes/' /etc/default/dnsmasq
    else
        echo "IGNORE_RESOLVCONF=yes" >> /etc/default/dnsmasq
    fi
else
    echo "IGNORE_RESOLVCONF=yes" > /etc/default/dnsmasq
fi
# env var มีผลเฉพาะตอน (re)start — restart ตรงนี้เลย ไม่พึ่ง side effect ตอน pigate boot
systemctl restart dnsmasq || log_warn "ไม่สามารถ restart dnsmasq ได้ (จะถูก restart อีกครั้งตอน pigate เริ่มทำงาน)"
log_ok "ตั้งค่า IGNORE_RESOLVCONF=yes ใน /etc/default/dnsmasq สำเร็จ"

# =============================================================================
# STEP 2.2: สร้าง systemd template service สำหรับ dhcpcd (dhcpcd@.service)
# =============================================================================
# เหตุผลที่แยก dhcpcd ออกมาเป็น service ของตัวเอง (รันเป็น root ปกติ) แทนที่จะให้
# pigate เรียก dhcpcd ตรง ๆ ผ่าน sudo หรือผ่าน setcap:
#   dhcpcd ใช้กลไก privilege separation ภายในตัวเอง (chroot + setuid + setgid
#   เพื่อลดสิทธิ์ตัวเองหลัง bind socket) ซึ่งต้องการ CAP_SYS_CHROOT/CAP_SETUID/
#   CAP_SETGID ครบทั้ง 3 ตัว การให้ 3 สิทธิ์นี้กับ pigate.service โดยตรงจะทำให้
#   pigate เองมีสิทธิ์ setuid(0) กลับเป็น root ได้ตลอดเวลา ซึ่งเป็นความเสี่ยงที่
#   ไม่จำเป็น จึงแยก dhcpcd ให้รันเป็น root ของตัวเองในอีก service หนึ่ง แล้วให้
#   pigate สั่ง start/stop/restart ผ่าน systemctl (ยืนยันตัวตนด้วย polkit ใน
#   STEP 3 แทน sudo) แทนที่ pigate จะต้องมีสิทธิ์ root ติดตัวเอง
log_info "STEP 2.2: สร้าง dhcpcd@.service..."

DHCPCD_BIN="$(command -v dhcpcd || true)"
if [[ -z "${DHCPCD_BIN}" ]]; then
    log_error "ไม่พบ dhcpcd ในระบบ กรุณาติดตั้งก่อน (apt-get install -y dhcpcd5 หรือ dhcpcd)"
    exit 1
fi

DHCPCD_SERVICE_FILE="/etc/systemd/system/dhcpcd@.service"
cat > "${DHCPCD_SERVICE_FILE}" << EOF
[Unit]
Description=dhcpcd on %I
Wants=network.target
Before=network.target
After=network-pre.target
BindsTo=sys-subsystem-net-devices-%i.device

[Service]
Type=simple
ExecStart=${DHCPCD_BIN} -B -q -f /var/lib/pigate/dhcpcd.conf %I
ExecStop=${DHCPCD_BIN} -k %I
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

log_ok "สร้างไฟล์ ${DHCPCD_SERVICE_FILE} สำเร็จ (ใช้ dhcpcd binary: ${DHCPCD_BIN})"

# หมายเหตุ: ไม่ enable/start unit ตัวนี้ตรงนี้ เพราะชื่อ interface (เช่น
# wlx0cef1548ff2b) ขึ้นกับฮาร์ดแวร์ของแต่ละเครื่อง — ตัว pigate binary เองจะเป็น
# คนสั่ง `systemctl start dhcpcd@<interface>.service` ตอน runtime ตามที่เจอ
# interface จริง

systemctl daemon-reload
log_ok "daemon-reload สำหรับ dhcpcd@.service สำเร็จ"

# =============================================================================
# STEP 3: สร้าง polkit rule สำหรับ wpa_supplicant, dnsmasq และ dhcpcd
# =============================================================================
log_info "STEP 3: สร้าง polkit rule..."

POLKIT_RULE_FILE="/etc/polkit-1/rules.d/10-pigate-system.rules"
mkdir -p /etc/polkit-1/rules.d

cat > "${POLKIT_RULE_FILE}" << 'EOF'
polkit.addRule(function(action, subject) {
    // ดักจับเฉพาะคำสั่ง manage-units ที่มาจาก User 'pigate'
    if (action.id == "org.freedesktop.systemd1.manage-units" && subject.user == "pigate") {
        var unit = action.lookup("unit");
        
        // 1. ถ้าเป็น Service ที่อนุญาต -> ให้ผ่าน (YES)
        //    dhcpcd@ ใช้ prefix match เพราะชื่อ interface ต่อท้ายไม่แน่นอน
        //    (ขึ้นกับฮาร์ดแวร์ เช่น wlan0, wlx0cef1548ff2b) เหมือน wpa_supplicant@
        if (unit.indexOf("wpa_supplicant@") === 0 ||
            unit.indexOf("dhcpcd@") === 0 ||
            unit === "systemd-resolved.service" ||
            unit === "systemd-timesyncd.service" ||
            unit === "dnsmasq.service" ||
            unit === "pigate.service") {
            return polkit.Result.YES;
        }
        // 2. ถ้าเป็น Service ตัวอื่นๆ นอกเหนือจากด้านบน -> ปฏิเสธทันที (NO)
        else {
            return polkit.Result.NO;
        }
    }

    // ดักจับ action ของ org.freedesktop.hostname1, org.freedesktop.timedate1 และ
    // org.freedesktop.login1 แยกต่างหาก (คนละ action id กับ systemd1.manage-units
    // ด้านบน) เพื่ออนุญาตให้ pigate ตั้งชื่อเครื่องผ่าน hostnamed, ตั้งเขตเวลา/NTP/เวลา
    // ผ่าน timedated และสั่ง reboot/shutdown ผ่าน logind โดยไม่ต้อง exec
    // `hostnamectl` / `timedatectl` / `reboot` / `shutdown`
    //
    // *-multiple-sessions จำเป็นเผื่อกรณีมี user session อื่นค้างอยู่ (เช่น SSH) —
    // logind จะสลับไปตรวจ action ตัวนี้แทน reboot/power-off ปกติ
    if ((action.id == "org.freedesktop.hostname1.set-static-hostname" ||
         action.id == "org.freedesktop.hostname1.set-hostname" ||
         action.id == "org.freedesktop.timedate1.set-timezone" ||
         action.id == "org.freedesktop.timedate1.set-ntp" ||
         action.id == "org.freedesktop.timedate1.set-time" ||
         action.id == "org.freedesktop.login1.reboot" ||
         action.id == "org.freedesktop.login1.reboot-multiple-sessions" ||
         action.id == "org.freedesktop.login1.power-off" ||
         action.id == "org.freedesktop.login1.power-off-multiple-sessions") &&
        subject.user == "pigate") {
        return polkit.Result.YES;
    }
    else {
        return polkit.Result.NO;
    }
});
EOF

log_ok "สร้างไฟล์ ${POLKIT_RULE_FILE} สำเร็จ"

systemctl restart polkit
log_ok "restart polkit สำเร็จ"

# =============================================================================
# STEP 4: สร้าง directories สำหรับ pigate
# =============================================================================
log_info "STEP 4: สร้าง directories..."

mkdir -p /var/lib/pigate
mkdir -p /run/pigate
chown -R pigate:netdev /var/lib/pigate
chown -R pigate:pigate /run/pigate
chmod 775 /var/lib/pigate
log_ok "สร้าง /var/lib/pigate และ /run/pigate สำเร็จ"

# สร้างไฟล์ baseline dhcpcd.conf ที่ pigate เป็นเจ้าของ (อ่านโดย dhcpcd@.service
# ผ่าน -f ดู STEP 2.2) หากยังไม่มี — ค่าเริ่มต้นคือไม่ share hostname (ว่าง/มีแต่
# comment) ตรงกับค่า default ของ system_hostname_settings.share_with_dhcp = 0
DHCPCD_CONF_FILE="/var/lib/pigate/dhcpcd.conf"
if [[ ! -f "${DHCPCD_CONF_FILE}" ]]; then
    cat > "${DHCPCD_CONF_FILE}" << 'EOF'
# Managed by PiGate. Do not edit manually.
EOF
    chown pigate:netdev "${DHCPCD_CONF_FILE}"
    chmod 0644 "${DHCPCD_CONF_FILE}"
    log_ok "สร้างไฟล์ ${DHCPCD_CONF_FILE} สำเร็จ"
fi

# dhcpcd persists its DUID, IPv6 privacy secret, and lease files under /var/lib/dhcpcd.
# ตั้งแต่ปรับให้ dhcpcd รันผ่าน dhcpcd@.service เป็น root ของตัวเอง (ดู STEP 2.2)
# ไดเรกทอรีนี้ไม่จำเป็นต้องเป็นของ user pigate อีกต่อไป (root เขียนได้อยู่แล้ว
# โดยไม่ต้องพึ่ง ownership) แต่คง chown ไว้เผื่อ dhcpcd ถูกเรียกแบบ manual/debug
# โดย user pigate โดยตรงในบางกรณี ไม่ทำให้เกิดปัญหาเพิ่มเติม
# mkdir -p /var/lib/dhcpcd
# chown -R pigate:netdev /var/lib/dhcpcd
# log_ok "สร้าง /var/lib/dhcpcd สำเร็จ"

# =============================================================================
# STEP 5: ตั้งค่า DNS Config directory
# =============================================================================
log_info "STEP 5: ตั้งค่า systemd-resolved config directory..."

mkdir -p /etc/systemd/resolved.conf.d
setfacl -m u:pigate:rwx /etc/systemd/resolved.conf.d
setfacl -d -m u:pigate:rwx /etc/systemd/resolved.conf.d
log_ok "ตั้งค่า /etc/systemd/resolved.conf.d สำเร็จ"

# =============================================================================
# STEP 5.1: ตั้งค่า systemd-timesyncd drop-in directory (สำหรับ NTP Server)
# =============================================================================
# timedate1 ไม่มี API สำหรับตั้ง NTP server เอง — pigate จึงเขียนไฟล์ drop-in
# /etc/systemd/timesyncd.conf.d/50-pigate.conf แบบ atomic (temp+rename) แล้ว
# restart systemd-timesyncd ผ่าน D-Bus (อนุญาตใน polkit STEP 3 แล้ว)
log_info "STEP 5.1: ตั้งค่า systemd-timesyncd config directory..."

if ! systemctl list-unit-files 2>/dev/null | grep -q '^systemd-timesyncd\.service'; then
    log_warn "ไม่พบ systemd-timesyncd — ฟีเจอร์กำหนด NTP Server เองจะไม่ทำงาน"
    log_warn "ติดตั้งด้วย: apt-get install systemd-timesyncd"
fi

mkdir -p /etc/systemd/timesyncd.conf.d
setfacl -m u:pigate:rwx /etc/systemd/timesyncd.conf.d
setfacl -d -m u:pigate:rwx /etc/systemd/timesyncd.conf.d
# สร้างไฟล์ drop-in เปล่าไว้ล่วงหน้า (pigate เป็นเจ้าของ) — ค่าเริ่มต้นคือไม่มี
# directive NTP= (ใช้ค่า default ของ distro) จนกว่าผู้ใช้จะกำหนด NTP Server เอง
TIMESYNCD_DROPIN="/etc/systemd/timesyncd.conf.d/50-pigate.conf"
if [[ ! -f "${TIMESYNCD_DROPIN}" ]]; then
    cat > "${TIMESYNCD_DROPIN}" << 'EOF'
# Managed by PiGate. Do not edit manually.
EOF
fi
chown pigate:netdev "${TIMESYNCD_DROPIN}"
chmod 0644 "${TIMESYNCD_DROPIN}"
log_ok "ตั้งค่า /etc/systemd/timesyncd.conf.d สำเร็จ"

# =============================================================================
# STEP 6: คัดลอก binary และตั้งค่า capabilities
# =============================================================================
log_info "STEP 6: ติดตั้ง binary..."

cp "${BINARY_SRC}" /usr/local/bin/pigate
chmod 755 /usr/local/bin/pigate
log_ok "คัดลอก pigate ไปยัง /usr/local/bin/ สำเร็จ"

log_info "ตั้งค่า Linux capabilities (cap_net_admin, cap_net_raw)..."
setcap cap_net_admin,cap_net_raw+ep /usr/local/bin/pigate
log_ok "ตั้งค่า capabilities สำเร็จ"

# หมายเหตุ: ไม่ setcap ให้ตัว dhcpcd binary โดยตรงอีกต่อไป เพราะตอนนี้ dhcpcd
# ถูกเรียกผ่าน dhcpcd@.service (รันเป็น root เต็มรูปแบบ ดู STEP 2.2) ไม่ได้ถูก
# pigate exec ตรง ๆ แบบเดิมแล้ว การ setcap cap_net_admin,cap_net_raw ให้ binary
# dhcpcd อย่างเดียว (ไม่มี CAP_SYS_CHROOT/CAP_SETUID/CAP_SETGID) ยังทำให้
# privilege separation ภายใน dhcpcd ทำงานไม่สมบูรณ์อยู่ดี (ดู log
# ps_dropprivs / failed to drop privileges) จึงตัดออกเพื่อไม่ให้เข้าใจผิดว่า
# เป็นวิธีที่ถูกต้อง

# =============================================================================
# STEP 7: สร้าง systemd service
# =============================================================================
log_info "STEP 7: สร้าง systemd service..."

SERVICE_FILE="/etc/systemd/system/pigate.service"
cat > "${SERVICE_FILE}" << 'EOF'
[Unit]
Description=PiGate Firewall & Network Manager
Documentation=https://github.com/saprayworld/pigate
After=network.target network-online.target
Wants=network-online.target

[Service]
User=pigate
Group=netdev
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
RuntimeDirectory=pigate dhcpcd
RuntimeDirectoryMode=0755
ExecStart=/usr/local/bin/pigate -mock=false -db=/var/lib/pigate/pigate.db
Restart=on-failure
RestartSec=5s

# Security hardening
NoNewPrivileges=false
ProtectSystem=false
ProtectHome=true

[Install]
WantedBy=multi-user.target
EOF

log_ok "สร้างไฟล์ service: ${SERVICE_FILE} สำเร็จ"

# Reload และ enable service
systemctl daemon-reload
systemctl enable pigate.service
log_ok "เปิดใช้งาน pigate.service สำเร็จ"

# =============================================================================
# เริ่ม service อีกครั้งถ้าเคยรันอยู่ก่อน Update
# =============================================================================
if [[ "${IS_UPDATE}" == true ]] && [[ "${SERVICE_WAS_RUNNING}" == true ]]; then
    log_info "กำลังเริ่ม ${SERVICE_NAME} อีกครั้ง..."
    systemctl start "${SERVICE_NAME}"
    log_ok "เริ่ม ${SERVICE_NAME} สำเร็จ"
fi

# =============================================================================
# สรุปผล
# =============================================================================
echo ""
if [[ "${IS_UPDATE}" == true ]]; then
    echo -e "${GREEN}=============================================${NC}"
    echo -e "${GREEN}    อัปเดต PiGate สำเร็จ! 🔄              ${NC}"
    echo -e "${GREEN}=============================================${NC}"
else
    echo -e "${GREEN}=============================================${NC}"
    echo -e "${GREEN}       ติดตั้ง PiGate สำเร็จ! 🎉           ${NC}"
    echo -e "${GREEN}=============================================${NC}"
fi
echo ""
echo -e "  Binary:   ${BLUE}/usr/local/bin/pigate${NC}"
echo -e "  Database: ${BLUE}/var/lib/pigate/pigate.db${NC}"
echo -e "  Service:  ${BLUE}/etc/systemd/system/pigate.service${NC}"
echo -e "  dhcpcd:   ${BLUE}/etc/systemd/system/dhcpcd@.service${NC} (per-interface, ควบคุมผ่าน polkit)"
echo ""
echo -e "${YELLOW}คำสั่งถัดไป:${NC}"
if [[ "${IS_UPDATE}" == true ]] && [[ "${SERVICE_WAS_RUNNING}" == true ]]; then
    echo -e "  ดู status:         sudo systemctl status pigate"
    echo -e "  ดู logs:           sudo journalctl -u pigate -f"
else
    echo -e "  เริ่มต้น service:  sudo systemctl start pigate"
    echo -e "  ดู status:         sudo systemctl status pigate"
    echo -e "  ดู logs:           sudo journalctl -u pigate -f"
fi
echo ""