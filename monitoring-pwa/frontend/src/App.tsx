import { useState, useEffect, useCallback } from 'react'
import { Activity, History, Settings, Bell, Server, CheckCircle, AlertTriangle, Copy, Check, LayoutDashboard, Skull, ChevronRight } from 'lucide-react'

// 通知履歴レコードの型定義である。
interface NotificationItem {
	id: number
	title: string
	body: string
	url?: string
	level: 'info' | 'success' | 'warning' | 'error'
	created_at: string
}

export default function App() {
	const [activeTab, setActiveTab] = useState<'dashboard' | 'history' | 'settings'>('dashboard')
	const [online, setOnline] = useState(navigator.onLine)
	const [history, setHistory] = useState<NotificationItem[]>([])
	const [isSubscribed, setIsSubscribed] = useState(false)
	const [submitting, setSubmitting] = useState(false)
	const [logs, setLogs] = useState<string[]>([])
	const [copied, setCopied] = useState(false)

	// ローカル開発時に別ポートで動くバックエンドを指すように、Viteの環境変数でURLを定義する。
	const apiBase = import.meta.env.DEV ? 'http://localhost:8080' : ''

	// システムログの追加用メソッドである。
	const log = useCallback((msg: string) => {
		setLogs((prev) => [`[${new Date().toLocaleTimeString()}] ${msg}`, ...prev])
	}, [])

	// ブラウザのオンライン・オフライン監視を設定する。
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
		const cmd = `curl -X POST https://monitoring.cluster.wpc/webhook/generic \\\n  -H "Content-Type: application/json" \\\n  -H "Authorization: Bearer <WEBHOOK_TOKEN>" \\\n  -d '{"title": "タイトル", "body": "本文", "level": "info"}'`
		navigator.clipboard.writeText(cmd)
		setCopied(true)
		setTimeout(() => setCopied(false), 2000)
	}

	return (
		<>
			<header className="app-header">
				<h1 className="app-title">Nuage Monitor</h1>
				<div className="connection-badge">
					<span className={`connection-dot ${online ? 'online' : ''}`}></span>
					{online ? 'オンライン' : 'オフライン'}
				</div>
			</header>

			<main className="app-content">
				{activeTab === 'dashboard' && (
					<>
						<div className="card" style={{ padding: '0.75rem', overflow: 'hidden' }}>
							<h2 className="card-title" style={{ marginBottom: '0.75rem' }}><Server size={18} />ノード健全性</h2>
							<iframe
								src="/grafana/d/nuage-node-health/node-health-overview?orgId=1&kiosk"
								width="100%"
								height="360"
								style={{ border: 'none', borderRadius: 'var(--radius-md)', backgroundColor: 'transparent' }}
								title="Grafana Node Health Dashboard"
							></iframe>
						</div>

						<a href="/grafana" target="_blank" rel="noopener noreferrer" className="card link-card">
							<h2 className="card-title"><LayoutDashboard size={18} />Grafana</h2>
							<ChevronRight size={18} className="link-card-arrow" />
						</a>

						<a href="/chaos-monitor" target="_blank" rel="noopener noreferrer" className="card link-card">
							<h2 className="card-title"><Skull size={18} />Chaos Monitor</h2>
							<ChevronRight size={18} className="link-card-arrow" />
						</a>
					</>
				)}

				{activeTab === 'history' && (
					<div className="card">
						<h2 className="card-title"><History size={18} />通知履歴</h2>
						<div className="history-list">
							{history.length === 0 ? (
								<div className="empty-state">履歴は存在しない。</div>
							) : (
								history.map((item) => (
									<div key={item.id} className={`history-item ${item.level}`}>
										<div className="history-icon-wrapper">
											{item.level === 'error' && <AlertTriangle size={16} />}
											{item.level === 'warning' && <AlertTriangle size={16} />}
											{item.level === 'success' && <CheckCircle size={16} />}
											{item.level === 'info' && <Bell size={16} />}
										</div>
										<div className="history-content">
											<div className="history-header">
												<span className="history-item-title">{item.title}</span>
												<span className="history-time">{new Date(item.created_at).toLocaleTimeString()}</span>
											</div>
											<span className="history-body">{item.body}</span>
											{item.url && (
												<a href={item.url} target="_blank" rel="noopener noreferrer" className="history-link">
													詳細を表示
												</a>
											)}
										</div>
									</div>
								))
							)}
						</div>
					</div>
				)}

				{activeTab === 'settings' && (
					<>
						<div className="card">
							<h2 className="card-title"><Settings size={18} />通知設定</h2>
							<div className="settings-section">
								<div className="settings-row">
									<div className="settings-label">
										<span className="label-title">プッシュ通知</span>
										<span className="label-desc">この端末でクラスタのアラートを受信する。</span>
									</div>
									<button
										onClick={isSubscribed ? unsubscribe : subscribe}
										disabled={submitting}
										className={`btn ${submitting ? 'btn-disabled' : 'btn-primary'}`}
									>
										{isSubscribed ? '解除する' : '有効にする'}
									</button>
								</div>

								{isSubscribed && (
									<div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '0.5rem' }}>
										<button onClick={sendTestNotify} className="btn btn-primary" style={{ fontSize: '0.75rem', padding: '0.5rem 0.75rem' }}>
											テスト通知を送信
										</button>
									</div>
								)}
							</div>
						</div>

						<div className="card">
							<h2 className="card-title"><Bell size={18} />汎用 Webhook URL</h2>
							<div className="settings-section">
								<span className="label-desc">
									GitHub Actions や任意の Cron ジョブ等から、以下のエンドポイントを叩くことでプッシュ通知をトリガーできる。
								</span>
								<div className="webhook-code-block">
									<pre>
										{`curl -X POST https://monitoring.cluster.wpc/webhook/generic \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer <WEBHOOK_TOKEN>" \\
  -d '{"title": "タイトル", "body": "本文", "level": "info"}'`}
									</pre>
									<button onClick={copyWebhookCommand} className="copy-btn">
										{copied ? <Check size={12} /> : <Copy size={12} />}
									</button>
								</div>
							</div>
						</div>

						{logs.length > 0 && (
							<div className="card" style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
								<h3 style={{ marginBottom: '0.5rem', color: 'var(--text-primary)' }}>システムログ</h3>
								<div style={{ maxHeight: '120px', overflowY: 'auto', fontFamily: 'monospace' }}>
									{logs.map((logItem, idx) => (
										<div key={idx}>{logItem}</div>
									))}
								</div>
							</div>
						)}
					</>
				)}
			</main>

			<nav className="app-nav">
				<button
					onClick={() => setActiveTab('dashboard')}
					className={`nav-item ${activeTab === 'dashboard' ? 'active' : ''}`}
				>
					<Activity size={20} />
					ダッシュボード
				</button>
				<button
					onClick={() => setActiveTab('history')}
					className={`nav-item ${activeTab === 'history' ? 'active' : ''}`}
				>
					<History size={20} />
					履歴
				</button>
				<button
					onClick={() => setActiveTab('settings')}
					className={`nav-item ${activeTab === 'settings' ? 'active' : ''}`}
				>
					<Settings size={20} />
					設定
				</button>
			</nav>
		</>
	)
}
