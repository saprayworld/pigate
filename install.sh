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

# =============================================================================
# STEP 3: สร้าง polkit rule สำหรับ wpa_supplicant และ dnsmasq
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
        if (unit.indexOf("wpa_supplicant@") === 0 || 
            unit === "systemd-resolved.service" ||
            unit === "dnsmasq.service" ||
            unit === "pigate.service") {
            return polkit.Result.YES;
        } 
        // 2. ถ้าเป็น Service ตัวอื่นๆ นอกเหนือจากด้านบน -> ปฏิเสธทันที (NO)
        else {
            return polkit.Result.NO;
        }
    }
});
EOF

log_ok "สร้างไฟล์ ${POLKIT_RULE_FILE} สำเร็จ"

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

# dhcpcd persists its DUID, IPv6 privacy secret, and lease files under /var/lib/dhcpcd.
# pigate runs dhcpcd directly (no sudo/root) with only cap_net_admin/cap_net_raw, so this
# directory must be writable by the pigate user or dhcpcd silently fails to persist state
# and IPv6 (SLAAC) does not start on DHCP-client interfaces.
mkdir -p /var/lib/dhcpcd
chown -R pigate:netdev /var/lib/dhcpcd
log_ok "สร้าง /var/lib/dhcpcd สำเร็จ"

# =============================================================================
# STEP 5: ตั้งค่า DNS Config directory
# =============================================================================
log_info "STEP 5: ตั้งค่า systemd-resolved config directory..."

mkdir -p /etc/systemd/resolved.conf.d
setfacl -m u:pigate:rwx /etc/systemd/resolved.conf.d
setfacl -d -m u:pigate:rwx /etc/systemd/resolved.conf.d
log_ok "ตั้งค่า /etc/systemd/resolved.conf.d สำเร็จ"

# =============================================================================
# STEP 6: คัดลอก binary และตั้งค่า capabilities
# =============================================================================
log_info "STEP 6: ติดตั้ง binary..."

cp "${BINARY_SRC}" /usr/local/bin/pigate
chmod 755 /usr/local/bin/pigate
log_ok "คัดลอก pigate ไปยัง /usr/local/bin/ สำเร็จ"

log_info "ตั้งค่า Linux capabilities (cap_net_admin, cap_net_raw)..."
setcap cap_net_admin,cap_net_raw+ep /usr/local/bin/pigate
setcap cap_net_admin,cap_net_raw+ep /usr/sbin/dhcpcd
log_ok "ตั้งค่า capabilities สำเร็จ"

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
