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

