import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

// The meshd backend serves the API at the same origin in production. During
// development Vite proxies API calls to the locally running daemon.
const API_TARGET = process.env.MESHD_API_TARGET ?? 'http://localhost:8080'
// Proxy every meshd REST endpoint to the daemon during development; the SPA
// (hash-routed) and its assets are served by Vite. A regex over the endpoint
// roots keeps this from drifting as endpoints are added — anything not matched
// (assets, /, /icons, …) falls through to the dev server.
const API_PROXY_RE =
  '^/(status|health|setup|homes|nodes|active-home|home-selection|scan|enroll|topology|reset)(/|$)'

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
    proxy: {
      // secure:false lets the proxy talk to a daemon serving its own
      // self-signed TLS cert — e.g. pointing MESHD_API_TARGET at a real
      // device's https mesh listener (https://<device>:8081) during dev.
      [API_PROXY_RE]: { target: API_TARGET, changeOrigin: true, secure: false },
    },
  },
})
