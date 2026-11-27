import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      registerType: 'autoUpdate',
      injectRegister: 'inline',
      manifest: {
        name: 'Nuage Notify',
        short_name: 'Notify',
        description: 'Nuage cluster notification receiver.',
        theme_color: '#191816',
        background_color: '#191816',
        display: 'standalone',
        orientation: 'portrait',
        scope: '/',
        start_url: '/',
        icons: [
          {
            src: 'icons/icon-192.png',
            sizes: '192x192',
            type: 'image/png',
            purpose: 'any maskable'
          },
          {
            src: 'icons/icon-512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'any maskable'
          }
        ]
      }
    })
  ],
  build: {
    // 書体 (IBM Plex Sans JP は文字の範囲ごとの小さなファイルに分かれている) は CSS に埋め込まず、
    // 別のファイルのまま配って、使う文字の分だけ読み込ませる
    assetsInlineLimit: (file) => (/\.(woff2?|ttf|otf)$/.test(file) ? false : undefined)
  },
  server: {
    port: 3000,
    host: true
  }
})
