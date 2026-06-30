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
# STEP 3: สร้าง polkit rule สำหรับ wpa_supplicant
# =============================================================================
log_info "STEP 3: สร้าง polkit rule..."

POLKIT_RULE_FILE="/etc/polkit-1/rules.d/10-pigate-wpa.rules"
mkdir -p /etc/polkit-1/rules.d

cat > "${POLKIT_RULE_FILE}" << 'EOF'
polkit.addRule(function(action, subject) {
    // ดักจับเฉพาะคำสั่ง manage-units ที่มาจาก User 'pigate'
    if (action.id == "org.freedesktop.systemd1.manage-units" && subject.user == "pigate") {
        var unit = action.lookup("unit");
        
        // 1. ถ้าเป็น Service ที่อนุญาต -> ให้ผ่าน (YES)
        if (unit.indexOf("wpa_supplicant@") === 0 || 
            unit === "systemd-resolved.service" ||
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

# =============================================================================
# STEP 5: ตั้งค่า DNS Config directory
# =============================================================================
log_info "STEP 5: ตั้งค่า systemd-resolved config directory..."

mkdir -p /etc/systemd/resolved.conf.d
setfacl -m u:pigate:rwx /etc/systemd/resolved.conf.d
setfacl -d -m u:pigate:rwx /etc/systemd/resolved.conf.d
log_ok "ตั้งค่า /etc/systemd/resolved.conf.d สำเร็จ"

# =============================================================================
# STEP 6: ตั้งค่า sudoers สำหรับ pigate
# =============================================================================
log_info "STEP 6: ตั้งค่า sudoers rule..."

SUDOERS_FILE="/etc/sudoers.d/pigate"
cat > "${SUDOERS_FILE}" << 'EOF'
pigate ALL=(ALL) NOPASSWD: /usr/sbin/dhclient, /usr/sbin/dhcpcd
EOF

# ตรวจสอบว่าไฟล์ sudoers ถูกต้อง
if visudo -cf "${SUDOERS_FILE}"; then
    chmod 440 "${SUDOERS_FILE}"
    log_ok "สร้างไฟล์ sudoers: ${SUDOERS_FILE} สำเร็จ"
else
    log_error "ไฟล์ sudoers ไม่ถูกต้อง — กรุณาตรวจสอบ"
    rm -f "${SUDOERS_FILE}"
    exit 1
fi

# =============================================================================
# STEP 7: คัดลอก binary และตั้งค่า capabilities
# =============================================================================
log_info "STEP 7: ติดตั้ง binary..."

cp "${BINARY_SRC}" /usr/local/bin/pigate
chmod 755 /usr/local/bin/pigate
log_ok "คัดลอก pigate ไปยัง /usr/local/bin/ สำเร็จ"

log_info "ตั้งค่า Linux capabilities (cap_net_admin, cap_net_raw)..."
setcap cap_net_admin,cap_net_raw+ep /usr/local/bin/pigate
log_ok "ตั้งค่า capabilities สำเร็จ"

# =============================================================================
# STEP 8: สร้าง systemd service
# =============================================================================
log_info "STEP 8: สร้าง systemd service..."

SERVICE_FILE="/etc/systemd/system/pigate.service"
cat > "${SERVICE_FILE}" << 'EOF'
[Unit]
Description=PiGate Firewall & Network Manager
Documentation=https://github.com/saprayworld/pigate-frontend
After=network.target network-online.target
Wants=network-online.target

[Service]
User=pigate
Group=netdev
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
RuntimeDirectory=pigate
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
# สรุปผล
# =============================================================================
echo ""
echo -e "${GREEN}=============================================${NC}"
echo -e "${GREEN}       ติดตั้ง PiGate สำเร็จ! 🎉           ${NC}"
echo -e "${GREEN}=============================================${NC}"
echo ""
echo -e "  Binary:   ${BLUE}/usr/local/bin/pigate${NC}"
echo -e "  Database: ${BLUE}/var/lib/pigate/pigate.db${NC}"
echo -e "  Service:  ${BLUE}/etc/systemd/system/pigate.service${NC}"
echo ""
echo -e "${YELLOW}คำสั่งถัดไป:${NC}"
echo -e "  เริ่มต้น service:  sudo systemctl start pigate"
echo -e "  ดู status:         sudo systemctl status pigate"
echo -e "  ดู logs:           sudo journalctl -u pigate -f"
echo ""
