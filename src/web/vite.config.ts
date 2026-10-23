import { defineConfig, type ProxyOptions } from 'vite'
import react from '@vitejs/plugin-react'

const backendTarget = process.env.TANGENTRY_API_TARGET ?? 'http://127.0.0.1:7380'
const backendProxy: ProxyOptions = {
  target: backendTarget,
  configure(proxy) {
    proxy.on('proxyReq', (proxyReq, req) => {
      if (req.headers.host) proxyReq.setHeader('host', req.headers.host)
    })
  },
}

export default defineConfig({
  plugins: [react()],
  server: { port: 5173, strictPort: true, proxy: { '/api': backendProxy, '/login': backendProxy } },
})
