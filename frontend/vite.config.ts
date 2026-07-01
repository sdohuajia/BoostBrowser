import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'

const defaultDevPort = 5218

function resolveBoolean(rawValue: string | undefined, fallbackValue: boolean) {
  const raw = String(rawValue ?? '').trim().toLowerCase()
  if (!raw) {
    return fallbackValue
  }
  if (raw === '1' || raw === 'true' || raw === 'yes' || raw === 'on') {
    return true
  }
  if (raw === '0' || raw === 'false' || raw === 'no' || raw === 'off') {
    return false
  }
  return fallbackValue
}

function resolveDevPort() {
  const raw = Number.parseInt(process.env.FRONTEND_PORT || '', 10)
  if (Number.isInteger(raw) && raw > 0 && raw <= 65535) {
    return raw
  }
  return defaultDevPort
}

const devPort = resolveDevPort()
const disableHmr = resolveBoolean(process.env.FRONTEND_DISABLE_HMR, false)

export default defineConfig({
  plugins: [react()],
  server: {
    port: devPort,
    strictPort: true,
    host: '127.0.0.1',
    cors: true,
    hmr: disableHmr
      ? false
      : {
          host: '127.0.0.1',
          protocol: 'ws',
        },
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          'react-vendor': ['react', 'react-dom', 'react-router-dom'],
        },
      },
    },
  },
})

