import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The build goes to dist/app, which the Go binary embeds (see embed.go). During development, API calls are
// proxied to a locally running `osfm serve` (PocketBase's default port).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist/app',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8090',
      '/tiles': 'http://127.0.0.1:8090',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
})
