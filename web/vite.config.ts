import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

// The meshd backend serves the API at the same origin in production. During
// development Vite proxies API calls to the locally running daemon.
const API_TARGET = process.env.MESHD_API_TARGET ?? 'http://localhost:8080'
const API_PATHS = ['/status', '/health', '/homes', '/nodes']

export default defineConfig({
  plugins: [
    vue(),
    VitePWA({
      registerType: 'autoUpdate',
      manifest: {
        name: 'OpenWrt Mesh Manager',
        short_name: 'OMM',
        description: 'Local-first mesh networking management for OpenWrt',
        theme_color: '#0f172a',
        background_color: '#0f172a',
        display: 'standalone',
        start_url: './',
        icons: [
          {
            src: 'icons/icon-192.svg',
            sizes: '192x192',
            type: 'image/svg+xml',
            purpose: 'any maskable',
          },
          {
            src: 'icons/icon-512.svg',
            sizes: '512x512',
            type: 'image/svg+xml',
            purpose: 'any maskable',
          },
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  // Hash-based routing means the backend only ever serves index.html and
  // assets, so the app is built with relative asset paths.
  base: './',
  build: {
    // Keep the committed dist/.gitkeep placeholder (which lets //go:embed
    // all:dist compile without a frontend build) rather than wiping it on each
    // build. A fresh checkout's dist only contains the placeholder, so nothing
    // stale is left behind in CI.
    emptyOutDir: false,
  },
  server: {
    proxy: Object.fromEntries(
      API_PATHS.map((path) => [path, { target: API_TARGET, changeOrigin: true }]),
    ),
  },
})
