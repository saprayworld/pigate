import path from "path"
import tailwindcss from "@tailwindcss/vite"
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },

  // สำหรับรันทดสอบแยก Frontend ออกจาก Backend
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:2479', // ชี้ไปยังพอร์ตของ Go Backend
        changeOrigin: true,
        secure: false,
      }
    }
  }
})
