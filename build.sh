#!/bin/bash
set -e

echo "=== Building PiGate Project ==="

# 1. Build frontend
echo "Building React frontend..."
cd frontend
yarn build
cd ..

# 2. Prepare backend embed directory
echo "Syncing frontend build to backend embed directory..."
# Remove old build files in the embed directory
rm -rf backend/internal/api/dist
mkdir -p backend/internal/api/dist

# Copy new build files
cp -r frontend/dist/* backend/internal/api/dist/

# Recreate .gitkeep to prevent git status showing deleted folder/files
echo "# Placeholder to keep the folder in git and prevent compilation errors" > backend/internal/api/dist/.gitkeep

# 3. Build backend
echo "Building Go backend..."
cd backend
go build -o pigate-backend ./cmd/pigate
cd ..

cp ./backend/pigate-backend pigate

echo "=== Build Complete! Binary is available at backend/pigate-backend ==="
