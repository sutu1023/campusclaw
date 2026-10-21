import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The SPA is served by Nginx in production. During development Vite proxies
// /api to the Go service so the browser only ever talks to one origin.
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
