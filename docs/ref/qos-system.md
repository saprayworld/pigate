QoS System ที่ใช้อยู่ตอนนี้ (ไม่ใช่ในระบบที่กำลังทำ) โดยการรันคำสั่งนี้

sudo tc qdisc add dev eth0 root handle 1: htb default 10
sudo tc class add dev eth0 parent 1: classid 1:1 htb rate 50mbit ceil 50mbit
sudo tc filter add dev eth0 parent 1:0 protocol ip prio 1 u32 match ip src 172.24.25.0/24 flowid 1:1
sudo tc filter add dev eth0 parent 1:0 protocol ip prio 1 u32 match ip dst 172.24.25.0/24 flowid 1:1
sudo tc filter add dev eth0 parent 1:0 protocol ip prio 1 u32 match ip dst 0.0.0.0/0 flowid 1:1

บน Router RPI5 ตัวหลัก
มี Interface อยู่ 2 ขา คือ wlan0 กับ eth0
- ขา wlan0 ได้ปล่อยไปตามปกติ
- ขา eth0 จำกัดที่ขานี้แทนที่ความเร็ว 50Mbps

จากการทำผลคือได้ลิมิตความเร็วตอนดาวน์โหลดที่เครื่อง client ที่ 50Mbps แต่ตอนอัพโหลดไม่สามารถจำกัดได้เลย ได้ความเร็วเต็มความเร็วตลอด
ตอนนี้ก็ใช้งานได้แหละ เพราะไม่ค่อยได้อัพโหลดอะไรมาก

ที่ทำเช่นนี้เพราะจับกับเครือข่ายที่ใช้ร่วมกับคนอื่นจึงไม่อยากไปกินแบนด์วิธของคนอื่นเยอะเกินไป

ในส่วนนี้จะเป็นการอัพเดทโปรเจกต์ Pigate โดยการเพิ่มระบบ QoS เข้าไป

