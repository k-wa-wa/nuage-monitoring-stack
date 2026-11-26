import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { setupIonicReact } from '@ionic/react'
import { ios26ThemeConfig } from '@rdlabo/ionic-theme-ios26'

/* Ionic の土台 */
import '@ionic/react/css/core.css'
import '@ionic/react/css/normalize.css'
import '@ionic/react/css/structure.css'
import '@ionic/react/css/typography.css'
import '@ionic/react/css/padding.css'
import '@ionic/react/css/flex-utils.css'
import '@ionic/react/css/display.css'
/* iOS 26 (Liquid Glass) 風のテーマ。ライト・ダークは端末の設定に合わせる */
import '@rdlabo/ionic-theme-ios26/styles/system.css'
/* このアプリの調整 (文字の大きさ・メッセージ行など) */
import './theme/app.css'

import App from './App.tsx'
import { installViewportDebug } from './utils/viewportDebug'

// Apple の標準に揃えるため、どの端末でも iOS の見た目と動きにする
setupIonicReact({
	...ios26ThemeConfig,
	mode: 'ios'
})

// 実機での高さ・スクロールの計測用 (一時的。?debug=viewport か設定画面で有効になる)
installViewportDebug()

createRoot(document.getElementById('root')!).render(
	<StrictMode>
		<App />
	</StrictMode>
)
