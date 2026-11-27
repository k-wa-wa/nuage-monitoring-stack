import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

/* 書体 (Nuage UI 規約: IBM Plex Sans / IBM Plex Sans JP / IBM Plex Mono)。日本語は文字の範囲ごとに分かれ、使う分だけ読み込まれる */
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@fontsource/ibm-plex-sans-jp/400.css'
import '@fontsource/ibm-plex-sans-jp/500.css'
import '@fontsource/ibm-plex-sans-jp/600.css'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
/* @nuage/ui のトークン・土台・部品 */
import '@nuage/ui/styles.css'
/* このアプリ固有の見た目 (メッセージ行・本文の Markdown など) */
import './app.css'

import App from './App.tsx'
import { installViewportDebug } from './utils/viewportDebug'

// 実機での高さ・スクロールの計測用 (一時的。?debug=viewport か設定画面で有効になる)
installViewportDebug()

createRoot(document.getElementById('root')!).render(
	<StrictMode>
		<App />
	</StrictMode>
)
