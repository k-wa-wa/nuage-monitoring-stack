import type { ReactNode } from 'react'
import { useDocumentScrollLock } from './useDocumentScrollLock'
import { useViewportFix } from './useViewportFix'

interface AppShellProps {
	children: ReactNode
}

/**
 * アプリの外枠である。iPhone の PWA で次をまとめて手当てする。
 * - 画面全体を動かさない (html/body の固定と、スクロールできない場所のタッチを止める)
 * - キーボードが開いている間は、見えている高さに合わせる (送信欄をキーボードのすぐ上に出す)
 *
 * index.html 側では、次を前提にする (iPhone 実機で確かめた値):
 * - <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover, interactive-widget=resizes-content">
 * - <meta name="apple-mobile-web-app-status-bar-style" content="black">
 *   (black-translucent はホーム画面 PWA で画面の下にステータスバー分の描けない帯が残る)
 */
export function AppShell({ children }: AppShellProps) {
	useViewportFix()
	useDocumentScrollLock()
	return <div className="nui-app">{children}</div>
}
