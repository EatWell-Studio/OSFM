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
      // Our own service worker (src/sw/sw.ts; docs/architecture.md §4.5), into which the build injects the list of
      // files to precache. The app registers it (src/pwa/UpdatePrompt.tsx) and offers to reload when a new version
      // is waiting; the reload activates it through the service worker's SKIP_WAITING message.
      strategies: 'injectManifest',
      srcDir: 'src/sw',
      filename: 'sw.ts',
      injectRegister: false,
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
      injectManifest: {
        // The app shell, the MapLibre worker (.mjs), the glyphs (.pbf) and the sprites (.json, .png): the same for every
        // member, cache first, replaced with each release. The licence texts beside the glyphs are not needed.
        globPatterns: ['**/*.{html,js,mjs,css,png,json,pbf}'],
        rollupFormat: 'iife',
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
