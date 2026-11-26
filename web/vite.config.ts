import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'
import { defineConfig } from 'vitest/config'

const themeColor = '#c2410c'
const backgroundColor = '#ffffff'

// The build goes to dist/app, which the Go binary embeds (see embed.go). During development, API calls are
// proxied to a locally running `osfm serve` (PocketBase's default port).
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      // registerSW.js, a separate file: the content security policy allows no inline script (docs/architecture.md
      // §4.7). A new version waits until every tab of the app is closed; the "new version, refresh" prompt of §4.5
      // comes with the offline work and activates it through the service worker's SKIP_WAITING message.
      injectRegister: 'script',
      registerType: 'prompt',
      // The icons are in public/ and so in the precache already (globPatterns).
      includeManifestIcons: false,
      manifest: {
        id: '/',
        name: 'OSFM',
        short_name: 'OSFM',
        lang: 'zh-CN',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        theme_color: themeColor,
        background_color: backgroundColor,
        icons: [
          { src: '/pwa-192x192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
          { src: '/pwa-512x512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
          { src: '/maskable-icon-512x512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        // Only the app shell is precached; offline data and tiles are later work (§4.5).
        globPatterns: ['**/*.{html,js,css,png}'],
        cleanupOutdatedCaches: true,
        // Client-side routes are answered with the cached index.html, but never the server's own paths: the API, the
        // tiles, PocketBase's admin UI and the instance metadata must always reach the network.
        navigateFallback: '/index.html',
        navigateFallbackDenylist: [/^\/api(\/|$)/, /^\/tiles(\/|$)/, /^\/_(\/|$)/, /^\/\.well-known(\/|$)/],
      },
    }),
  ],
  resolve: {
    tsconfigPaths: true,
  },
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
