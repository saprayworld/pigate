### คำสั่งทดสอบ

```bash
cd backend

# Build
go build -o pigate-backend ./cmd/pigate

# Start
./pigate-backend -port=8081 -db=pigate.db -mock=true

# Start in Read-only mode
./pigate-backend -port=8081 -db=pigate.db -mock=true -disable-edit=true


# -----

cd frontend
yarn dev
```

ใช้ user `admin` และ pass `admin` เพื่อทดทอบ

### ติดตั้ง Go

```bash
wget https://go.dev/dl/go1.26.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.4.linux-amd64.tar.gz

echo "export PATH=\$PATH:/usr/local/go/bin" >> ~/.bashrc

source ~/.bashrc

go version
```

> ควรปิดและเปิด session terminal ใหม่ เพื่อให้ go path มีผล

