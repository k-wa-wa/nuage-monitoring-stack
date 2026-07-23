import { useState, useEffect, useCallback } from 'react'
import { Activity, History, Settings as SettingsIcon } from 'lucide-react'
import Dashboard from './components/Dashboard'
import Settings from './components/Settings'
import HistoryItem from './components/HistoryItem'

// 通知履歴レコードの型定義である。
interface NotificationItem {
	id: number
	title: string
	body: string
	url?: string
	level: 'info' | 'success' | 'warning' | 'error'
	details?: string
	created_at: string
}

export default function App() {
	// パスからタブを取得するヘルパーである。
	const getTabFromPath = (path: string): 'dashboard' | 'history' | 'settings' => {
		if (path.startsWith('/history')) return 'history'
		if (path.startsWith('/settings')) return 'settings'
		return 'dashboard'
	}

	const [activeTab, setActiveTab] = useState<'dashboard' | 'history' | 'settings'>(() =>
		getTabFromPath(window.location.pathname)
	)
	const [expandedId, setExpandedId] = useState<number | null>(null)
	const [online, setOnline] = useState(navigator.onLine)
	const [history, setHistory] = useState<NotificationItem[]>([])
	const [isSubscribed, setIsSubscribed] = useState(false)
	const [submitting, setSubmitting] = useState(false)
	const [logs, setLogs] = useState<string[]>([])
	const [copied, setCopied] = useState(false)

	// ローカル開発時に別ポートで動くバックエンドを指すように、Viteの環境変数でURLを定義する。
	const apiBase = import.meta.env.DEV ? 'http://localhost:8080' : ''
	// 開発環境と本番環境で Grafana のベースURLを切り替える。
	const grafanaBase = import.meta.env.DEV ? 'https://monitoring.cluster.wpc' : ''

	// システムログの追加用メソッドである。
	const log = useCallback((msg: string) => {
		setLogs((prev) => [`[${new Date().toLocaleTimeString()}] ${msg}`, ...prev])
	}, [])

	// ルーティングを処理する navigateTo メソッドである。
	const navigateTo = (tab: 'dashboard' | 'history' | 'settings', extraPath = '') => {
		setActiveTab(tab)
		const newPath = tab === 'dashboard' ? '/' : `/${tab}${extraPath}`
		if (window.location.pathname !== newPath) {
			window.history.pushState(null, '', newPath)
		}
	}

	// ブラウザ of オンライン・オフライン監視を設定する。
	useEffect(() => {
		const handleOnline = () => setOnline(true)
		const handleOffline = () => setOnline(false)
		window.addEventListener('online', handleOnline)
		window.addEventListener('offline', handleOffline)
		return () => {
			window.removeEventListener('online', handleOnline)
			window.removeEventListener('offline', handleOffline)
		}
	}, [])

	// 過去の通知履歴取得処理である。
	const fetchHistory = useCallback(async () => {
		try {
			const res = await fetch(`${apiBase}/api/history`)
			if (res.ok) {
				const data = await res.json()
				setHistory(data)
			}
		} catch (err: any) {
			console.error('Failed to fetch history:', err)
		}
	}, [apiBase])

	// 初期処理である。
	useEffect(() => {
		fetchHistory()
	}, [fetchHistory])

	// 履歴タブを開いた際に最新の履歴をフェッチする。
	useEffect(() => {
		if (activeTab === 'history') {
			fetchHistory()
		}
	}, [activeTab, fetchHistory])

	// URL パスのディープリンク（/history/:id）を検知して表示を更新する。
	useEffect(() => {
		const path = window.location.pathname
		if (path.startsWith('/history/')) {
			const idStr = path.substring('/history/'.length)
			const id = parseInt(idStr, 10)
			if (!isNaN(id)) {
				setActiveTab('history')
				setExpandedId(id)

				if (history.length > 0) {
					setTimeout(() => {
						const el = document.getElementById(`history-item-${id}`)
						if (el) {
							el.scrollIntoView({ behavior: 'smooth', block: 'center' })
						}
					}, 150)
				}
			}
		}
	}, [history])

	// 戻る・進むボタンのイベント（popstate）を監視して state を同期する。
	useEffect(() => {
		const handlePopState = () => {
			const path = window.location.pathname
			const tab = getTabFromPath(path)
			setActiveTab(tab)
			if (path.startsWith('/history/')) {
				const idStr = path.substring('/history/'.length)
				const id = parseInt(idStr, 10)
				if (!isNaN(id)) {
					setExpandedId(id)
				}
			} else {
				setExpandedId(null)
			}
		}
		window.addEventListener('popstate', handlePopState)
		return () => window.removeEventListener('popstate', handlePopState)
	}, [])

	// プッシュ通知の購読状態チェックである。
	const checkSubscription = useCallback(async () => {
		if ('serviceWorker' in navigator && 'PushManager' in window) {
			try {
				const reg = await navigator.serviceWorker.ready
				const sub = await reg.pushManager.getSubscription()
				setIsSubscribed(!!sub)
			} catch (err: any) {
				log(`購読チェック失敗: ${err.message}`)
			}
		}
	}, [log])

	useEffect(() => {
		checkSubscription()
	}, [checkSubscription])

	// Base64文字列を Uint8Array にデコードするヘルパーである。
	const urlBase64ToUint8Array = (base64String: string) => {
		const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
		const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
		const rawData = window.atob(base64)
		const outputArray = new Uint8Array(rawData.length)
		for (let i = 0; i < rawData.length; ++i) {
			outputArray[i] = rawData.charCodeAt(i)
		}
		return outputArray
	}

	// プッシュ通知の購読処理である。
	const subscribe = async () => {
		if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
			alert('Web Push未対応ブラウザである(iOSの場合はホーム画面にPWAを追加する必要がある)。')
			return
		}

		setSubmitting(true)
		try {
			const permission = await Notification.requestPermission()
			if (permission !== 'granted') {
				log(`通知が拒否された: ${permission}`)
				return
			}

			const reg = await navigator.serviceWorker.ready
			const vapidRes = await fetch(`${apiBase}/api/vapid-public-key`)
			if (!vapidRes.ok) throw new Error('VAPID公開鍵の取得に失敗した。')
			const { publicKey } = await vapidRes.json()

			const sub = await reg.pushManager.subscribe({
				userVisibleOnly: true,
				applicationServerKey: urlBase64ToUint8Array(publicKey)
			})

			const saveRes = await fetch(`${apiBase}/api/subscribe`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(sub)
			})

			if (!saveRes.ok) throw new Error('サーバーへの購読登録に失敗した。')

			setIsSubscribed(true)
			log('通知の購読登録が完了した。')
		} catch (err: any) {
			log(`エラー: ${err.message}`)
		} finally {
			setSubmitting(false)
		}
	}

	// 購読解除処理である。
	const unsubscribe = async () => {
		setSubmitting(true)
		try {
			const reg = await navigator.serviceWorker.ready
			const sub = await reg.pushManager.getSubscription()
			if (sub) {
				await sub.unsubscribe()
				await fetch(`${apiBase}/api/unsubscribe`, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ endpoint: sub.endpoint })
				})
			}
			setIsSubscribed(false)
			log('通知の購読を解除した。')
		} catch (err: any) {
			log(`エラー: ${err.message}`)
		} finally {
			setSubmitting(false)
		}
	}

	// テスト通知の送信要求である。
	const sendTestNotify = async () => {
		try {
			const res = await fetch(`${apiBase}/api/test-notify`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					title: 'テスト通知',
					body: 'これは monitoring-pwa からのテスト通知である。',
					level: 'success'
				})
			})
			if (res.ok) {
				log('テスト通知の送信を要求した。')
			} else {
				log(`送信要求失敗: ${res.status}`)
			}
		} catch (err: any) {
			log(`エラー: ${err.message}`)
		}
	}

	// Webhook の curl コマンドをクリップボードにコピーする。
	const copyWebhookCommand = () => {
		const cmd = `curl -X POST https://monitoring.cluster.wpc/webhook/generic \\\n  -H "Content-Type: application/json" \\\n  -d '{"title": "エラー検知", "body": "エラーが発生した。", "level": "error", "details": "Error: something went wrong\\n  at main.js:10:5"}'`
		navigator.clipboard.writeText(cmd)
		setCopied(true)
		setTimeout(() => setCopied(false), 2000)
	}

	// iframe 内で読み込まれた場合は描画を制限する（無限ネスト防止）。
	if (window.self !== window.top) {
		return (
			<div style={{ padding: '20px', textAlign: 'center', color: 'var(--text-secondary)' }}>
				Nuage Monitor is loaded inside an iframe.
			</div>
		)
	}

	return (
		<>
			<header className="app-header">
				<h1 className="app-title">
					<span className="app-logo"></span>Nuage Monitor
				</h1>
				<div className="connection-badge">
					<span className={`connection-dot ${online ? 'online' : ''}`}></span>
					{online ? 'オンライン' : 'オフライン'}
				</div>
			</header>

			<main className="app-content">
				{activeTab === 'dashboard' && <Dashboard grafanaBase={grafanaBase} />}

				{activeTab === 'history' && (
					<div className="card">
						<h2 className="card-title">
							<History size={18} />
							通知履歴
						</h2>
						<div className="history-list">
							{history.length === 0 ? (
								<div className="empty-state">履歴は存在しない。</div>
							) : (
								history.map((item) => (
									<HistoryItem
										key={item.id}
										item={item}
										isExpanded={expandedId === item.id}
										onToggle={() => {
											const nextId = expandedId === item.id ? null : item.id
											setExpandedId(nextId)
											navigateTo('history', nextId ? `/${nextId}` : '')
										}}
										grafanaBase={grafanaBase}
									/>
								))
							)}
						</div>
					</div>
				)}

				{activeTab === 'settings' && (
					<Settings
						isSubscribed={isSubscribed}
						submitting={submitting}
						onSubscribeToggle={isSubscribed ? unsubscribe : subscribe}
						onSendTestNotify={sendTestNotify}
						copyWebhookCommand={copyWebhookCommand}
						copied={copied}
						logs={logs}
					/>
				)}
			</main>

			<nav className="app-nav">
				<button
					onClick={() => navigateTo('dashboard')}
					className={`nav-item ${activeTab === 'dashboard' ? 'active' : ''}`}
				>
					<Activity size={20} />
					ダッシュボード
				</button>
				<button
					onClick={() => navigateTo('history')}
					className={`nav-item ${activeTab === 'history' ? 'active' : ''}`}
				>
					<History size={20} />
					履歴
				</button>
				<button
					onClick={() => navigateTo('settings')}
					className={`nav-item ${activeTab === 'settings' ? 'active' : ''}`}
				>
					<SettingsIcon size={20} />
					設定
				</button>
			</nav>
		</>
	)
}
