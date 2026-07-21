### ติดตั้ง Go

```bash
wget https://go.dev/dl/go1.26.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.4.linux-amd64.tar.gz

echo "export PATH=\$PATH:/usr/local/go/bin" >> ~/.bashrc

source ~/.bashrc

go version
```

> ควรปิดและเปิด session terminal ใหม่ เพื่อให้ go path มีผล

### คำสั่งทดสอบ

```bash
cd backend

# Build
go build -o pigate-backend ./cmd/pigate

# Add Permission
sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend

# Start
./pigate-backend -port=8081 -db=pigate.db -mock=true

# Start in Read-only mode
./pigate-backend -port=8081 -db=pigate.db -mock=true -disable-edit=true


# -----

cd frontend
yarn dev
```

ใช้ user `admin` และ pass `admin` เพื่อทดทอบ

### CLI For test

สามารถใช้คำสั่งเหล่านี้เพื่อทดสอบการทำงาน โดยรันใน session terminal ใหม่

#### create VLAN Interface

```bash
sudo ip link add link eth0 name eth0.300 type vlan id 300
```

#### Assign IP Address to VLAN Interface

```bash
sudo ip addr add 172.26.0.1/24 dev eth0.300
```

#### Remove IP Address from VLAN Interface

```bash
sudo ip addr del 172.26.0.1/24 dev eth0.300
```

## AI Prompt

```
คุณคือโปรแกรมเมอร์ที่เชี่ยวชาญการเขียนโปรแกรมภาษา GoLang และ React
```

```
ขณะนี้เรากำลังทำงานกันใน WSL ซึ่งฟังก์ชั่นบางอย่างอาจจะไม่พร้อมใช้งาน หรือทดสอบไม่ได้ ให้ทดสอบเท่าที่จะทำได้ การทดสอบบนเครื่องจริงผมจะเป็นคนทำเอง
```

```
วางแผนการทำ <เป้าหมาย> โดยมีข้อมูลว่า ควรทำอะไรบ้างเป็นขั้นตอน ทำที่ไหน แก้ไฟล์อะไรบ้าง และข้อควรระวัง

เขียนแผนงานไปที่ <target-file>
```

```
โจทย์: [อธิบายฟีเจอร์]. 

พวกคุณคือทีมโปรแกรมเมอร์ที่เชี่ยวชาญการเขียนโปรแกรมภาษา GoLang และ React
มอบให้คุณคือเลขาหรือผู้ประสานงานในทีม
ก่อนเริ่มงาน ให้คุณตรวจสอบ git branch ก่อนว่าอยู่ที่ main ไหม หากไม่อยู่ให้ถามผมก่อนว่าจะใช่ branch ไหน หรือต้องการสลับไปยัง main ก่อนไหม
หลังจากนั้น ประเมินงานว่าต้องใช้ทีมงานเมื่อ ต้องสำรวจโค้ด วางแผน งานที่ใหญ่ การลงมือแก้โค้ดเป็นส่วนใหญ่ การเพิ่มฟีเจอร์ และอื่นๆ อันนี้ต้องใช้ ai-tech-lead 
ประเมินเป็นว่าไม่ต้องใช้ทีมงานเมื่อ แก้โค้ดเล็กๆ ไม่กี่บรรทัด

เดิน pipeline ให้: ai-tech-lead วางแผน → ai-developer ลงมือทำทุก task → ai-qa ตรวจ, loop แก้บั๊กกับ developer ไม่เกิน 2 รอบต่อ task, ครบแล้วเด้งกลับ tech-lead. หยุดมาถามผมเมื่อ (1) แผนเสร็จ ก่อนเริ่มโค้ด และ (2) มี task ที่ loop ครบ 2 รอบแล้วไม่ผ่าน
```

#### เป้าหมาย

ควรอธิบายเป้าหมายให้ชัดเจน ให้ละเอียดที่สุดเท่าที่จะทำได้ เช่น

```
เราจะเพิ่มข้อมูล Metric เข้าไปในการตั้งค่า Interface เมื่อมีการตั้งค่า Metric นี้ ระบบจะนำค่านี้ไปทำ routing default gateway ให้โดยอัตโนมัติ 
ไฟล์ที่เกี่ยวข้องเบื้องต้นมีดังนี้ file1 file2
ช่วยสำรวจโค้ดเหล่านี้และวางแผนว่า ...
```
