### Setting
- Hostname
- Share hostname with dhcp client


### DHCP Server
- สามารถตั้งค่า DHCP เป็นราย Interface ได้
- เมื่อจะทำการเริ่มต้น Service DHCP จะต้องตรวจสอบก่อนว่ามี Interface จริงไหม ถ้าไม่มีให้ข้าม

### DNS Server (ไม่ใช่ DNS System)
- สามารถจัดการ zone หลักได้ (เพิ่ม, แก้ไข, ลบ)
  - ชื่อ zone เช่น pigate.local, home.sapray.net
  - forward rule
  - กำหนด ip ที่อนุญาติได้
- สามารถจัดการที่อยู่ DNS Record ภายใต้ zone ได้
  - host1     A      172.24.29.22
  - service1  CNAME  host1

