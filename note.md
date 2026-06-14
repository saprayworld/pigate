### คำสั่งทดสอบ

```bash
cd backend

# Build
go build -o pigate-backend ./cmd/pigate

# Start
./pigate-backend -port=8081 -db=pigate.db -mock=true

# -----

cd frontend
yarn dev
```

ใช้ user `admin` และ pass `admin` เพื่อทดทอบ
