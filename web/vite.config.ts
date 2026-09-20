import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 前端 dev server：:5173，API 代理到本地 backend :8080
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
  build: { outDir: 'dist' },
})
