import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

// 初期描画で使う ASCII・ひらがな・カタカナ・基本漢字の woff2 を preload して、フォント差し替え時のわずかなガタつきを抑える
const PRELOAD_FONT_PATTERN =
	/^assets\/(?:ibm-plex-sans-latin-(?:400|500|600)|ibm-plex-sans-jp-(?:117|118|119)-(?:400|500|600))-normal-.*\.woff2$/

function fontPreloadPlugin(): Plugin {
	return {
		name: 'nuage-font-preload',
		transformIndexHtml: {
			order: 'post',
			handler(html, ctx) {
				if (!ctx.bundle) return html
				const links = Object.keys(ctx.bundle)
					.filter((fileName) => PRELOAD_FONT_PATTERN.test(fileName))
					.sort()
					.map(
						(fileName) =>
							`\t\t<link rel="preload" as="font" type="font/woff2" href="/${fileName}" crossorigin />`
					)
				if (links.length === 0) return html
				return html.replace('</head>', `${links.join('\n')}\n\t</head>`)
			}
		}
	}
}

// https://vite.dev/config/
export default defineConfig({
	plugins: [
		react(),
		fontPreloadPlugin(),
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
