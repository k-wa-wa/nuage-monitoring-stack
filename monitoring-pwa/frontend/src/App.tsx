import { useState, useEffect, useCallback, useMemo } from 'react'
import { Bell, Settings as SettingsIcon } from 'lucide-react'
import Settings from './components/Settings'
import NotificationItem from './components/NotificationItem'
import { GRAFANA_BASE_URL, parseAppRoute } from './utils/url'
import type { UnifiedNotification } from './types/notification'

type NotificationFilter = 'all' | 'firing' | 'resolved' | 'notice'

export default function App() {
	const initialRoute = useMemo(() => parseAppRoute(window.location.pathname), [])

	const [activeTab, setActiveTab] = useState<'notifications' | 'settings'>(initialRoute.tab)
	const [expandedUid, setExpandedUid] = useState<string | null>(initialRoute.selectedUid)
	const [online, setOnline] = useState(navigator.onLine)
	const [notifications, setNotifications] = useState<UnifiedNotification[]>([])
	const [deepLinkedItem, setDeepLinkedItem] = useState<UnifiedNotification | null>(null)
	const [filter, setFilter] = useState<NotificationFilter>('all')
	const [isSubscribed, setIsSubscribed] = useState(false)
	const [submitting, setSubmitting] = useState(false)
	const [logs, setLogs] = useState<string[]>([])

	// ローカル開発時に別ポートで動くバックエンドを指すように URL を切り替える。
	const apiBase = import.meta.env.DEV ? 'http://localhost:8080' : ''

	const log = useCallback((msg: string) => {
		setLogs((prev) => [`[${new Date().toLocaleTimeString()}] ${msg}`, ...prev])
	}, [])

	// 一覧に無い古いディープリンク対象も表示できるようにマージする。
	const allNotifications = useMemo(() => {
		if (!deepLinkedItem || notifications.some((n) => n.uid === deepLinkedItem.uid)) {
			return notifications
		}
		return [deepLinkedItem, ...notifications]
	}, [notifications, deepLinkedItem])

	const displayedNotifications = useMemo(() => {
		switch (filter) {
			case 'firing':
				return allNotifications.filter((n) => n.kind === 'alert' && n.status === 'firing')
			case 'resolved':
				return allNotifications.filter((n) => n.kind === 'alert' && n.status === 'resolved')
			case 'notice':
				return allNotifications.filter((n) => n.kind === 'notification')
			default:
				return allNotifications
		}
	}, [allNotifications, filter])

	const firingCount = useMemo(
		() => allNotifications.filter((n) => n.kind === 'alert' && n.status === 'firing').length,
		[allNotifications]
	)
	const resolvedCount = useMemo(
		() => allNotifications.filter((n) => n.kind === 'alert' && n.status === 'resolved').length,
		[allNotifications]
	)
	const noticeCount = useMemo(
		() => allNotifications.filter((n) => n.kind === 'notification').length,
		[allNotifications]
	)

	const navigateToPath = useCallback((newPath: string) => {
		const parsed = parseAppRoute(newPath)
		setActiveTab(parsed.tab)
		setExpandedUid(parsed.selectedUid)
		if (parsed.selectedUid || parsed.legacyHistoryId !== null) {
			setFilter('all')
		}
		if (window.location.pathname !== newPath) {
			window.history.pushState(null, '', newPath)
		}
	}, [])

	const navigateToTab = (tab: 'notifications' | 'settings') => {
		navigateToPath(tab === 'settings' ? '/settings' : '/')
	}

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

	// 統合通知一覧の取得処理である。
	const fetchNotifications = useCallback(async () => {
		try {
			const res = await fetch(`${apiBase}/api/notifications`)
			if (res.ok) {
				const data: UnifiedNotification[] = await res.json()
				setNotifications(data)
			}
		} catch (err) {
			console.error('Failed to fetch notifications:', err)
		}
	}, [apiBase])

	useEffect(() => {
		fetchNotifications()
		const timer = setInterval(fetchNotifications, 15000)
		return () => clearInterval(timer)
	}, [fetchNotifications])

	useEffect(() => {
		if (activeTab === 'notifications') {
			fetchNotifications()
		}
	}, [activeTab, fetchNotifications])

	// ディープリンク時に一覧へ含まれない通知を単体取得し、対象要素へスクロールする。
	useEffect(() => {
		const route = parseAppRoute(window.location.pathname)
		if (route.tab !== 'notifications') return

		if (route.legacyHistoryId !== null && notifications.length > 0) {
			const alertMatch = notifications.find((n) => n.uid === `alert-${route.legacyHistoryId}`)
			const noteMatch = notifications.find((n) => n.uid === `notification-${route.legacyHistoryId}`)
			const target = alertMatch || noteMatch
			if (target) {
				setExpandedUid(target.uid)
				setFilter('all')
			}
			return
		}

		if (route.detailKind && route.detailId !== null) {
			const targetUid = `${route.detailKind}-${route.detailId}`
			const existsInList = notifications.some((n) => n.uid === targetUid)
			if (!existsInList) {
				fetch(`${apiBase}/api/notifications/${route.detailKind}/${route.detailId}`)
					.then((res) => (res.ok ? res.json() : null))
					.then((item: UnifiedNotification | null) => {
						if (item) {
							setDeepLinkedItem(item)
							setExpandedUid(item.uid)
							setFilter('all')
						}
					})
					.catch((err) => console.error('Failed to fetch notification detail:', err))
			}
		}
	}, [apiBase, notifications, expandedUid])

	// 展開中の要素へ自動スクロールする。
	useEffect(() => {
		if (!expandedUid || activeTab !== 'notifications') return
		const timer = setTimeout(() => {
			const el = document.getElementById(`notification-item-${expandedUid}`)
			if (el) {
				el.scrollIntoView({ behavior: 'smooth', block: 'center' })
			}
		}, 150)
		return () => clearTimeout(timer)
	}, [expandedUid, activeTab, allNotifications.length])

	// 戻る・進むボタン (popstate) と Service Worker からの通知クリックメッセージを受信する。
	useEffect(() => {
		const handlePopState = () => {
			const parsed = parseAppRoute(window.location.pathname)
			setActiveTab(parsed.tab)
			setExpandedUid(parsed.selectedUid)
		}
		const handleSwMessage = (event: MessageEvent) => {
			if (event.data?.type === 'NOTIFICATION_CLICK' && typeof event.data.url === 'string') {
				navigateToPath(event.data.url)
				fetchNotifications()
			}
		}
		window.addEventListener('popstate', handlePopState)
		if ('serviceWorker' in navigator) {
			navigator.serviceWorker.addEventListener('message', handleSwMessage)
		}
		return () => {
			window.removeEventListener('popstate', handlePopState)
			if ('serviceWorker' in navigator) {
				navigator.serviceWorker.removeEventListener('message', handleSwMessage)
			}
		}
	}, [navigateToPath, fetchNotifications])

	// プッシュ通知の購読状態チェックである。
	const checkSubscription = useCallback(async () => {
		if ('serviceWorker' in navigator && 'PushManager' in window) {
			try {
				const reg = await navigator.serviceWorker.ready
				const sub = await reg.pushManager.getSubscription()
				setIsSubscribed(!!sub)
			} catch (err: unknown) {
				const message = err instanceof Error ? err.message : String(err)
				log(`購読チェック失敗: ${message}`)
			}
		}
	}, [log])

	useEffect(() => {
		checkSubscription()
	}, [checkSubscription])

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
		} catch (err: unknown) {
			const message = err instanceof Error ? err.message : String(err)
			log(`エラー: ${message}`)
		} finally {
			setSubmitting(false)
		}
	}

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
		} catch (err: unknown) {
			const message = err instanceof Error ? err.message : String(err)
			log(`エラー: ${message}`)
		} finally {
			setSubmitting(false)
		}
	}

	const sendTestNotify = async () => {
		try {
			const res = await fetch(`${apiBase}/api/test-notify`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					title: 'テスト通知',
					body: 'これは Nuage Notify からのテスト通知である。',
					level: 'success'
				})
			})
			if (res.ok) {
				log('テスト通知の送信を要求した。')
			} else {
				log(`送信要求失敗: ${res.status}`)
			}
		} catch (err: unknown) {
			const message = err instanceof Error ? err.message : String(err)
			log(`エラー: ${message}`)
		}
	}

	const handleToggleItem = (item: UnifiedNotification) => {
		const isClosing = expandedUid === item.uid
		if (isClosing) {
			setExpandedUid(null)
			if (window.location.pathname !== '/') {
				window.history.pushState(null, '', '/')
			}
			return
		}

		setExpandedUid(item.uid)
		if (item.uid.startsWith('alert-')) {
			window.history.pushState(null, '', `/alerts/${item.id}`)
		} else if (item.uid.startsWith('notification-')) {
			window.history.pushState(null, '', `/notifications/${item.id}`)
		}
	}

	// iframe 内で読み込まれた場合は描画を制限する（無限ネスト防止）。
	if (window.self !== window.top) {
		return (
			<div style={{ padding: '20px', textAlign: 'center', color: 'var(--text-secondary)' }}>
				Nuage Notify is loaded inside an iframe.
			</div>
		)
	}

	return (
		<>
			<header className="app-header">
				<h1 className="app-title">
					<span className="app-logo"></span>Nuage Notify
				</h1>
				<div className="connection-badge">
					<span className={`connection-dot ${online ? 'online' : ''}`}></span>
					{online ? 'オンライン' : 'オフライン'}
				</div>
			</header>

			<main className="app-content">
				{activeTab === 'notifications' && (
					<div className="card">
						<div className="panel-header history-panel-header">
							<h2 className="panel-title">
								<Bell size={16} />
								通知一覧
							</h2>
							<div className="filter-pill-group">
								<button
									className={`filter-pill ${filter === 'all' ? 'active' : ''}`}
									onClick={() => setFilter('all')}
								>
									すべて ({allNotifications.length})
								</button>
								<button
									className={`filter-pill ${filter === 'firing' ? 'active' : ''} ${firingCount > 0 ? 'pill-firing' : ''}`}
									onClick={() => setFilter('firing')}
								>
									発生中 ({firingCount})
								</button>
								<button
									className={`filter-pill ${filter === 'resolved' ? 'active' : ''}`}
									onClick={() => setFilter('resolved')}
								>
									解決済み ({resolvedCount})
								</button>
								<button
									className={`filter-pill ${filter === 'notice' ? 'active' : ''}`}
									onClick={() => setFilter('notice')}
								>
									一般通知 ({noticeCount})
								</button>
							</div>
						</div>
						<div className="history-list">
							{displayedNotifications.length === 0 ? (
								<div className="empty-state">
									{filter === 'firing'
										? '現在発生中のアラートは存在しない。'
										: filter === 'resolved'
										? '解決済みのアラートは存在しない。'
										: filter === 'notice'
										? '一般通知は存在しない。'
										: '通知履歴は存在しない。'}
								</div>
							) : (
								displayedNotifications.map((item) => (
									<NotificationItem
										key={item.uid}
										item={item}
										isExpanded={expandedUid === item.uid}
										onToggle={() => handleToggleItem(item)}
										grafanaBase={GRAFANA_BASE_URL}
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
						logs={logs}
					/>
				)}
			</main>

			<nav className="app-nav">
				<button
					onClick={() => navigateToTab('notifications')}
					className={`nav-item ${activeTab === 'notifications' ? 'active' : ''}`}
				>
					<Bell size={20} />
					通知一覧
				</button>
				<button
					onClick={() => navigateToTab('settings')}
					className={`nav-item ${activeTab === 'settings' ? 'active' : ''}`}
				>
					<SettingsIcon size={20} />
					設定
				</button>
			</nav>
		</>
	)
}
