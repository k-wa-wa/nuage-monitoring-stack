import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { ChevronLeft, Settings as SettingsIcon } from 'lucide-react'
import LoginScreen from './components/LoginScreen'
import ChannelSidebar from './components/ChannelSidebar'
import MessageList from './components/MessageList'
import ThreadPane from './components/ThreadPane'
import SearchModal from './components/SearchModal'
import Settings from './components/Settings'
import { useVisualViewport } from './hooks/useVisualViewport'
import { useDocumentScrollLock } from './hooks/useDocumentScrollLock'
import { useSwipeBack, type SwipeTargetLayer } from './hooks/useSwipeBack'
import { parseChatRoute } from './utils/url'
import { syncExistingPushSubscription } from './utils/pushSubscription'
import type {
	AuthUser,
	BootstrapResponse,
	BotInfo,
	Channel,
	ChatMessage,
	SearchResult
} from './types/chat'

type AuthState = 'loading' | 'unauthenticated' | 'authenticated'
type MobileScreen = 'channels' | 'messages' | 'thread'
type HistoryScreen = MobileScreen | 'settings'

interface ChatHistoryState {
	nuageDepth?: number
	screen?: HistoryScreen
	prevScreen?: HistoryScreen
}

const SLIDE_DURATION_MS = 280

function isMobileViewport(): boolean {
	return typeof window !== 'undefined' && window.innerWidth < 768
}

function prefersReducedMotion(): boolean {
	return (
		typeof window !== 'undefined' &&
		typeof window.matchMedia === 'function' &&
		window.matchMedia('(prefers-reduced-motion: reduce)').matches
	)
}

export default function App() {
	useVisualViewport()
	useDocumentScrollLock()

	const initialRoute = useMemo(() => parseChatRoute(window.location.pathname), [])
	const initialMobileScreen: MobileScreen = useMemo(() => {
		if (initialRoute.threadTs || initialRoute.deepLinkKind) return 'thread'
		if (initialRoute.channelId || initialRoute.view === 'settings') return 'messages'
		return 'channels'
	}, [initialRoute])

	const [authState, setAuthState] = useState<AuthState>('loading')
	const [user, setUser] = useState<AuthUser | null>(null)
	const [bot, setBot] = useState<BotInfo>({
		id: 'B01BOT',
		user_id: 'U01BOT',
		name: 'nuage-bot',
		online: false
	})
	const [channels, setChannels] = useState<Channel[]>([])
	const [activeView, setActiveView] = useState<'chat' | 'settings'>(initialRoute.view)
	const [activeChannelId, setActiveChannelId] = useState<string | null>(initialRoute.channelId)
	const [mobileScreen, setMobileScreen] = useState<MobileScreen>(initialMobileScreen)
	const [routePath, setRoutePath] = useState(() => window.location.pathname)

	const [messages, setMessages] = useState<ChatMessage[]>([])
	const [loadingMessages, setLoadingMessages] = useState(false)
	const [hasMore, setHasMore] = useState(false)
	const [loadingOlder, setLoadingOlder] = useState(false)
	const [sending, setSending] = useState(false)
	const [sendError, setSendError] = useState<string | null>(null)

	const [threadParent, setThreadParent] = useState<ChatMessage | null>(null)
	const [threadReplies, setThreadReplies] = useState<ChatMessage[]>([])
	const [loadingThread, setLoadingThread] = useState(false)
	const [threadEntering, setThreadEntering] = useState(false)
	const [threadExiting, setThreadExiting] = useState(false)
	const [settingsEntering, setSettingsEntering] = useState(false)
	const [settingsExiting, setSettingsExiting] = useState(false)
	const [searchOpen, setSearchOpen] = useState(false)

	// Push 通知設定関連
	const [isSubscribed, setIsSubscribed] = useState(false)
	const [submitting, setSubmitting] = useState(false)
	const [logs, setLogs] = useState<string[]>([])

	const chatShellRef = useRef<HTMLDivElement | null>(null)
	const activeChannelIdRef = useRef<string | null>(activeChannelId)
	const loadedChannelIdRef = useRef<string | null>(null)
	const threadParentTsRef = useRef<string | null>(null)
	const threadChannelIdRef = useRef<string | null>(null)
	const userIdRef = useRef<string | null>(null)
	const resolvedRouteRef = useRef<string | null>(null)
	const historyDepthRef = useRef<number>(
		typeof (window.history.state as ChatHistoryState | null)?.nuageDepth === 'number'
			? ((window.history.state as ChatHistoryState).nuageDepth ?? 0)
			: 0
	)
	const threadExitTimerRef = useRef<number | null>(null)
	const settingsExitTimerRef = useRef<number | null>(null)

	useEffect(() => {
		activeChannelIdRef.current = activeChannelId
	}, [activeChannelId])

	useEffect(() => {
		threadParentTsRef.current = threadParent?.ts || null
		threadChannelIdRef.current = threadParent?.channel_id || null
	}, [threadParent])

	useEffect(() => {
		userIdRef.current = user?.id || null
	}, [user])

	// 初期履歴エントリにアプリ独自の深さメタ情報を付与する（URL は維持）
	useEffect(() => {
		const curState = (window.history.state as ChatHistoryState | null) || {}
		if (typeof curState.nuageDepth !== 'number') {
			const screen: HistoryScreen =
				initialRoute.view === 'settings' ? 'settings' : initialMobileScreen
			window.history.replaceState(
				{ ...curState, nuageDepth: 0, screen } satisfies ChatHistoryState,
				'',
				window.location.pathname + window.location.search
			)
			historyDepthRef.current = 0
		}
	}, [initialRoute.view, initialMobileScreen])

	useEffect(() => {
		return () => {
			if (threadExitTimerRef.current !== null) {
				window.clearTimeout(threadExitTimerRef.current)
			}
			if (settingsExitTimerRef.current !== null) {
				window.clearTimeout(settingsExitTimerRef.current)
			}
		}
	}, [])

	const channelsById = useMemo(() => {
		const map: Record<string, string> = {}
		for (const ch of channels) {
			map[ch.id] = ch.name
		}
		return map
	}, [channels])

	// ローカル開発時に別ポートで動くバックエンドを指すように URL を切り替える。
	const apiBase = import.meta.env.DEV ? 'http://localhost:8080' : ''

	const log = useCallback((msg: string) => {
		setLogs((prev) => [`[${new Date().toLocaleTimeString()}] ${msg}`, ...prev])
	}, [])

	const cancelThreadExit = useCallback(() => {
		if (threadExitTimerRef.current !== null) {
			window.clearTimeout(threadExitTimerRef.current)
			threadExitTimerRef.current = null
		}
		setThreadExiting(false)
	}, [])

	const closeThreadWithAnimation = useCallback((immediate = false) => {
		if (threadExitTimerRef.current !== null) {
			window.clearTimeout(threadExitTimerRef.current)
			threadExitTimerRef.current = null
		}
		if (immediate || !isMobileViewport() || prefersReducedMotion()) {
			setThreadExiting(false)
			setThreadParent(null)
			setThreadReplies([])
			return
		}
		setThreadExiting(true)
		threadExitTimerRef.current = window.setTimeout(() => {
			threadExitTimerRef.current = null
			setThreadExiting(false)
			setThreadParent(null)
			setThreadReplies([])
		}, SLIDE_DURATION_MS)
	}, [])

	const triggerThreadEnterAnimation = useCallback(() => {
		if (!isMobileViewport() || prefersReducedMotion()) {
			setThreadEntering(false)
			return
		}
		setThreadEntering(true)
		window.requestAnimationFrame(() => {
			window.requestAnimationFrame(() => {
				setThreadEntering(false)
			})
		})
	}, [])

	const openSettingsView = useCallback(() => {
		if (settingsExitTimerRef.current !== null) {
			window.clearTimeout(settingsExitTimerRef.current)
			settingsExitTimerRef.current = null
		}
		setSettingsExiting(false)
		setActiveView('settings')
		setMobileScreen('messages')
		if (isMobileViewport() && !prefersReducedMotion()) {
			setSettingsEntering(true)
			window.requestAnimationFrame(() => {
				window.requestAnimationFrame(() => {
					setSettingsEntering(false)
				})
			})
		} else {
			setSettingsEntering(false)
		}
	}, [])

	const closeSettingsWithAnimation = useCallback((immediate = false) => {
		if (settingsExitTimerRef.current !== null) {
			window.clearTimeout(settingsExitTimerRef.current)
			settingsExitTimerRef.current = null
		}
		if (immediate || !isMobileViewport() || prefersReducedMotion()) {
			setSettingsExiting(false)
			setActiveView('chat')
			return
		}
		setSettingsExiting(true)
		settingsExitTimerRef.current = window.setTimeout(() => {
			settingsExitTimerRef.current = null
			setSettingsExiting(false)
			setActiveView('chat')
		}, SLIDE_DURATION_MS)
	}, [])

	const pushHistoryRoute = useCallback(
		(newPath: string, screen: HistoryScreen, prevScreen?: HistoryScreen) => {
			const nextDepth = historyDepthRef.current + 1
			historyDepthRef.current = nextDepth
			resolvedRouteRef.current = newPath
			setRoutePath(newPath)
			const nextState: ChatHistoryState = {
				nuageDepth: nextDepth,
				screen,
				prevScreen
			}
			if (window.location.pathname !== newPath) {
				window.history.pushState(nextState, '', newPath)
			} else {
				window.history.replaceState(nextState, '', newPath)
			}
		},
		[]
	)

	const markChannelRead = useCallback(
		async (channelId: string, ts?: string) => {
			try {
				await fetch(`${apiBase}/api/channels/${encodeURIComponent(channelId)}/read`, {
					method: 'POST',
					credentials: 'include',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ ts: ts || '' })
				})
				setChannels((prev) =>
					prev.map((ch) => (ch.id === channelId ? { ...ch, unread_count: 0 } : ch))
				)
			} catch {
				// ignore
			}
		},
		[apiBase]
	)

	const loadChannelMessages = useCallback(
		async (channelId: string) => {
			const isChannelSwitch = loadedChannelIdRef.current !== channelId
			if (isChannelSwitch) {
				loadedChannelIdRef.current = channelId
				setMessages([])
				setHasMore(false)
				setLoadingMessages(true)
			}
			try {
				const res = await fetch(
					`${apiBase}/api/channels/${encodeURIComponent(channelId)}/messages?limit=40`,
					{ credentials: 'include' }
				)
				if (res.status === 401) {
					setAuthState('unauthenticated')
					return
				}
				if (!res.ok) return
				const data: { messages: ChatMessage[]; has_more: boolean } = await res.json()
				if (activeChannelIdRef.current && activeChannelIdRef.current !== channelId) {
					return
				}
				setMessages(data.messages || [])
				setHasMore(Boolean(data.has_more))
				const latestTs =
					data.messages && data.messages.length > 0
						? data.messages[data.messages.length - 1].ts
						: undefined
				void markChannelRead(channelId, latestTs)
			} catch (err) {
				console.error('Failed to load messages:', err)
			} finally {
				setLoadingMessages(false)
			}
		},
		[apiBase, markChannelRead]
	)

	const loadThread = useCallback(
		async (channelId: string, threadTs: string) => {
			setLoadingThread(true)
			try {
				const res = await fetch(
					`${apiBase}/api/channels/${encodeURIComponent(channelId)}/threads/${encodeURIComponent(threadTs)}`,
					{ credentials: 'include' }
				)
				if (res.status === 401) {
					setAuthState('unauthenticated')
					return
				}
				if (!res.ok) return
				const data: { parent: ChatMessage; replies: ChatMessage[] } = await res.json()
				cancelThreadExit()
				setThreadParent(data.parent)
				setThreadReplies(data.replies || [])
				setMobileScreen('thread')
			} catch (err) {
				console.error('Failed to load thread:', err)
			} finally {
				setLoadingThread(false)
			}
		},
		[apiBase, cancelThreadExit]
	)

	const loadBootstrap = useCallback(async () => {
		try {
			const res = await fetch(`${apiBase}/api/bootstrap`, { credentials: 'include' })
			if (res.status === 401) {
				setAuthState('unauthenticated')
				setUser(null)
				return
			}
			if (!res.ok) {
				setAuthState('unauthenticated')
				return
			}
			const data: BootstrapResponse = await res.json()
			setUser(data.user)
			setChannels(data.channels || [])
			setBot(data.bot)
			setAuthState('authenticated')

			// 初期チャンネルが決まっていなければ最初のチャンネルを選ぶ
			setActiveChannelId((prev) => {
				if (prev && data.channels.some((c) => c.id === prev)) return prev
				return data.channels[0]?.id || null
			})
		} catch (err) {
			console.error('Bootstrap failed:', err)
			setAuthState('unauthenticated')
		}
	}, [apiBase])

	useEffect(() => {
		void loadBootstrap()
	}, [loadBootstrap])

	// 選択チャンネルが変わったらメッセージを取得する
	useEffect(() => {
		if (authState !== 'authenticated' || !activeChannelId || activeView !== 'chat') return
		void loadChannelMessages(activeChannelId)
	}, [authState, activeChannelId, activeView, loadChannelMessages])

	// ディープリンクや URL パスを解決する
	useEffect(() => {
		if (authState !== 'authenticated') return
		if (resolvedRouteRef.current === routePath) return

		const parsed = parseChatRoute(routePath)
		if (parsed.view === 'settings') {
			setSettingsExiting(false)
			setActiveView('settings')
			setMobileScreen('messages')
			closeThreadWithAnimation(true)
			resolvedRouteRef.current = routePath
			return
		}

		if (activeView === 'settings') {
			closeSettingsWithAnimation(false)
		} else {
			setActiveView('chat')
		}

		if (parsed.deepLinkKind && parsed.deepLinkId !== null) {
			resolvedRouteRef.current = routePath
			fetch(`${apiBase}/api/deeplink/${parsed.deepLinkKind}/${parsed.deepLinkId}`, {
				credentials: 'include'
			})
				.then((res) => (res.ok ? res.json() : null))
				.then((data: { channel_id: string; ts: string } | null) => {
					if (data && data.channel_id && data.ts) {
						setActiveChannelId(data.channel_id)
						void loadThread(data.channel_id, data.ts)
					}
				})
				.catch((err) => console.error('Failed to resolve deeplink:', err))
			return
		}

		if (parsed.channelId) {
			setActiveChannelId(parsed.channelId)
			if (parsed.threadTs) {
				if (!threadParentTsRef.current) {
					triggerThreadEnterAnimation()
				}
				void loadThread(parsed.channelId, parsed.threadTs)
			} else {
				if (threadParentTsRef.current) {
					closeThreadWithAnimation(false)
				}
				setMobileScreen('messages')
			}
			resolvedRouteRef.current = routePath
			return
		}

		// ルート '/' への遷移（popstate 等）
		if (threadParentTsRef.current) {
			closeThreadWithAnimation(false)
		}
		setMobileScreen('channels')
		resolvedRouteRef.current = routePath
	}, [
		authState,
		routePath,
		apiBase,
		activeView,
		loadThread,
		triggerThreadEnterAnimation,
		closeThreadWithAnimation,
		closeSettingsWithAnimation
	])

	// SSE でリアルタイム更新を受信し、再接続・復帰時に欠損を同期する
	useEffect(() => {
		if (authState !== 'authenticated') return

		const es = new EventSource(`${apiBase}/api/events`, { withCredentials: true })
		let openedOnce = false

		const resyncAfterReconnect = () => {
			void loadBootstrap()
			if (activeChannelIdRef.current) {
				void loadChannelMessages(activeChannelIdRef.current)
			}
			if (threadChannelIdRef.current && threadParentTsRef.current) {
				void loadThread(threadChannelIdRef.current, threadParentTsRef.current)
			}
		}

		es.addEventListener('open', () => {
			if (!openedOnce) {
				openedOnce = true
				return
			}
			resyncAfterReconnect()
		})

		const handleVisibilityOrOnline = () => {
			if (document.visibilityState === 'visible') {
				resyncAfterReconnect()
			}
		}
		document.addEventListener('visibilitychange', handleVisibilityOrOnline)
		window.addEventListener('online', resyncAfterReconnect)

		es.addEventListener('bot_status', (event: MessageEvent) => {
			try {
				const parsed = JSON.parse(event.data) as { payload?: { online?: boolean } }
				const online = Boolean(parsed.payload?.online)
				setBot((prev) => ({ ...prev, online }))
			} catch {
				// ignore
			}
		})

		es.addEventListener('message_created', (event: MessageEvent) => {
			try {
				const parsed = JSON.parse(event.data) as {
					channel_id?: string
					payload?: ChatMessage
				}
				const msg = parsed.payload
				if (!msg) return

				const curChannel = activeChannelIdRef.current
				const curThreadTs = threadParentTsRef.current
				const curUserId = userIdRef.current

				if (!msg.thread_ts) {
					// ルートメッセージ
					if (msg.channel_id === curChannel) {
						setMessages((prev) =>
							prev.some((m) => m.ts === msg.ts) ? prev : [...prev, msg]
						)
						void markChannelRead(msg.channel_id, msg.ts)
					} else if (msg.bot_id || msg.user_id !== curUserId) {
						setChannels((prev) =>
							prev.map((ch) =>
								ch.id === msg.channel_id
									? { ...ch, unread_count: ch.unread_count + 1, latest_ts: msg.ts }
									: ch
							)
						)
					}
				} else {
					// スレッド返信
					if (msg.channel_id === curChannel) {
						setMessages((prev) =>
							prev.map((m) =>
								m.ts === msg.thread_ts
									? { ...m, reply_count: (m.reply_count || 0) + 1, latest_reply: msg.ts }
									: m
							)
						)
						if (curThreadTs === msg.thread_ts) {
							setThreadReplies((prev) =>
								prev.some((r) => r.ts === msg.ts) ? prev : [...prev, msg]
							)
						}
						void markChannelRead(msg.channel_id, msg.ts)
					} else if (msg.bot_id || msg.user_id !== curUserId) {
						setChannels((prev) =>
							prev.map((ch) =>
								ch.id === msg.channel_id
									? { ...ch, unread_count: ch.unread_count + 1, latest_ts: msg.ts }
									: ch
							)
						)
					}
				}
			} catch {
				// ignore
			}
		})

		es.addEventListener('message_updated', (event: MessageEvent) => {
			try {
				const parsed = JSON.parse(event.data) as { payload?: ChatMessage }
				const updated = parsed.payload
				if (!updated) return

				setMessages((prev) => prev.map((m) => (m.ts === updated.ts ? updated : m)))
				setThreadParent((prev) => (prev && prev.ts === updated.ts ? updated : prev))
			} catch {
				// ignore
			}
		})

		return () => {
			document.removeEventListener('visibilitychange', handleVisibilityOrOnline)
			window.removeEventListener('online', resyncAfterReconnect)
			es.close()
		}
	}, [authState, apiBase, markChannelRead, loadBootstrap, loadChannelMessages, loadThread])

	const navigateToPath = useCallback(
		(newPath: string, screen: HistoryScreen = 'messages', prevScreen?: HistoryScreen) => {
			pushHistoryRoute(newPath, screen, prevScreen)
			resolvedRouteRef.current = null
		},
		[pushHistoryRoute]
	)

	// 戻る・進むボタンと Service Worker からの通知クリックを受信する
	useEffect(() => {
		const handlePopState = (event: PopStateEvent) => {
			const state = (event.state as ChatHistoryState | null) || null
			historyDepthRef.current = typeof state?.nuageDepth === 'number' ? state.nuageDepth : 0
			resolvedRouteRef.current = null
			setRoutePath(window.location.pathname)
		}
		const handleSwMessage = (event: MessageEvent) => {
			if (event.data?.type === 'NOTIFICATION_CLICK' && typeof event.data.url === 'string') {
				navigateToPath(event.data.url, 'thread')
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
	}, [navigateToPath])

	// プッシュ通知の購読状態チェックと既存購読のログインユーザー紐づけ同期
	const checkSubscription = useCallback(async () => {
		if ('serviceWorker' in navigator && 'PushManager' in window) {
			try {
				const reg = await navigator.serviceWorker.ready
				const sub = await reg.pushManager.getSubscription()
				setIsSubscribed(!!sub)
				if (sub) {
					await syncExistingPushSubscription(apiBase, sub, log)
				}
			} catch (err: unknown) {
				const message = err instanceof Error ? err.message : String(err)
				log(`購読チェック失敗: ${message}`)
			}
		}
	}, [apiBase, log])

	useEffect(() => {
		if (authState === 'authenticated') {
			void checkSubscription()
		}
	}, [authState, checkSubscription])

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
			const vapidRes = await fetch(`${apiBase}/api/vapid-public-key`, { credentials: 'include' })
			if (!vapidRes.ok) throw new Error('VAPID公開鍵の取得に失敗した。')
			const { publicKey } = await vapidRes.json()
			const sub = await reg.pushManager.subscribe({
				userVisibleOnly: true,
				applicationServerKey: urlBase64ToUint8Array(publicKey)
			})
			const saveRes = await fetch(`${apiBase}/api/subscribe`, {
				method: 'POST',
				credentials: 'include',
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
					credentials: 'include',
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
				credentials: 'include',
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

	const handleLogout = async () => {
		await fetch(`${apiBase}/api/auth/logout`, {
			method: 'POST',
			credentials: 'include'
		})
		setAuthState('unauthenticated')
		setUser(null)
		setMessages([])
		cancelThreadExit()
		setThreadParent(null)
		setThreadReplies([])
	}

	const handleSelectChannel = (channelId: string) => {
		const prevScreen: HistoryScreen =
			activeView === 'settings' ? 'settings' : mobileScreen
		closeSettingsWithAnimation(true)
		cancelThreadExit()
		setActiveView('chat')
		setActiveChannelId(channelId)
		setThreadParent(null)
		setThreadReplies([])
		setSendError(null)
		setMobileScreen('messages')
		const nextPath = `/channels/${encodeURIComponent(channelId)}`
		pushHistoryRoute(nextPath, 'messages', prevScreen)
	}

	const handleOpenThread = (msg: ChatMessage) => {
		const wasThreadOpen = Boolean(threadParent && !threadExiting)
		const prevScreen: HistoryScreen = wasThreadOpen ? 'thread' : 'messages'
		cancelThreadExit()
		if (!wasThreadOpen) {
			triggerThreadEnterAnimation()
		}
		setThreadParent(msg)
		setThreadReplies([])
		setMobileScreen('thread')
		const nextPath = `/channels/${encodeURIComponent(msg.channel_id)}/threads/${encodeURIComponent(msg.ts)}`
		pushHistoryRoute(nextPath, 'thread', prevScreen)
		void loadThread(msg.channel_id, msg.ts)
	}

	const handleCloseThread = useCallback(() => {
		const curState = window.history.state as ChatHistoryState | null
		const canPopBack =
			curState?.prevScreen === 'messages' &&
			typeof curState?.nuageDepth === 'number' &&
			curState.nuageDepth > 0

		setMobileScreen('messages')
		closeThreadWithAnimation(false)

		if (canPopBack) {
			window.history.back()
			return
		}

		const targetChannelId = threadParent?.channel_id || activeChannelId
		if (targetChannelId) {
			const nextPath = `/channels/${encodeURIComponent(targetChannelId)}`
			pushHistoryRoute(nextPath, 'messages', 'thread')
		}
	}, [threadParent, activeChannelId, closeThreadWithAnimation, pushHistoryRoute])

	const handleBackMobile = useCallback(() => {
		const curState = window.history.state as ChatHistoryState | null
		const canPopBack =
			curState?.prevScreen === 'channels' &&
			typeof curState?.nuageDepth === 'number' &&
			curState.nuageDepth > 0

		if (activeView === 'settings') {
			closeSettingsWithAnimation(false)
			if (!isMobileViewport()) {
				const targetPath = activeChannelId ? `/channels/${encodeURIComponent(activeChannelId)}` : '/'
				setMobileScreen('messages')
				pushHistoryRoute(targetPath, 'messages', 'settings')
				return
			}
			setMobileScreen('channels')
			if (canPopBack) {
				window.history.back()
				return
			}
			pushHistoryRoute('/', 'channels', 'settings')
			return
		}

		setMobileScreen('channels')
		if (canPopBack) {
			window.history.back()
			return
		}
		pushHistoryRoute('/', 'channels', 'messages')
	}, [activeView, activeChannelId, closeSettingsWithAnimation, pushHistoryRoute])

	const handleLoadOlder = async () => {
		if (!activeChannelId || loadingOlder || messages.length === 0) return
		const targetChannel = activeChannelId
		const oldestTs = messages[0].ts
		setLoadingOlder(true)
		try {
			const res = await fetch(
				`${apiBase}/api/channels/${encodeURIComponent(targetChannel)}/messages?limit=40&before=${encodeURIComponent(oldestTs)}`,
				{ credentials: 'include' }
			)
			if (!res.ok) return
			const data: { messages: ChatMessage[]; has_more: boolean } = await res.json()
			if (activeChannelIdRef.current !== targetChannel) return
			if (data.messages && data.messages.length > 0) {
				setMessages((prev) => {
					const existing = new Set(prev.map((m) => m.ts))
					const older = data.messages.filter((m) => !existing.has(m.ts))
					return [...older, ...prev]
				})
			}
			setHasMore(Boolean(data.has_more))
		} finally {
			setLoadingOlder(false)
		}
	}

	const handlePostMessage = async (text: string, threadTs?: string): Promise<boolean> => {
		const targetChannel = threadTs && threadParent ? threadParent.channel_id : activeChannelId
		if (!targetChannel) return false

		setSending(true)
		setSendError(null)
		try {
			const res = await fetch(
				`${apiBase}/api/channels/${encodeURIComponent(targetChannel)}/messages`,
				{
					method: 'POST',
					credentials: 'include',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ text, thread_ts: threadTs || '' })
				}
			)
			if (res.status === 401) {
				setAuthState('unauthenticated')
				return false
			}
			if (!res.ok) {
				setSendError(`送信に失敗した (HTTP ${res.status})。再度試してほしい。`)
				return false
			}
			const data: { message: ChatMessage; bot_online: boolean; delivered_to_bot: boolean } =
				await res.json()
			setBot((prev) => ({ ...prev, online: data.bot_online }))

			const created = data.message
			if (!created.thread_ts) {
				setMessages((prev) =>
					prev.some((m) => m.ts === created.ts) ? prev : [...prev, created]
				)
			} else {
				setThreadReplies((prev) =>
					prev.some((r) => r.ts === created.ts) ? prev : [...prev, created]
				)
			}
			return true
		} catch {
			setSendError('ネットワークエラーにより送信できなかった。接続を確認して再送してほしい。')
			return false
		} finally {
			setSending(false)
		}
	}

	const handleSelectSearchResult = (res: SearchResult) => {
		setSearchOpen(false)
		closeSettingsWithAnimation(true)
		setActiveView('chat')
		setActiveChannelId(res.channel_id)
		const targetThreadTs = res.message.thread_ts || res.message.ts
		triggerThreadEnterAnimation()
		setMobileScreen('thread')
		const nextPath = `/channels/${encodeURIComponent(res.channel_id)}/threads/${encodeURIComponent(targetThreadTs)}`
		pushHistoryRoute(nextPath, 'thread', 'channels')
		void loadThread(res.channel_id, targetThreadTs)
	}

	const swipeTargetLayer: SwipeTargetLayer = useMemo(() => {
		if (authState !== 'authenticated') return null
		if (mobileScreen === 'thread' && threadParent && !threadExiting) {
			return 'thread'
		}
		if (activeView === 'settings' && !settingsExiting) {
			return 'layer1'
		}
		if (mobileScreen === 'messages') {
			return 'layer1'
		}
		return null
	}, [authState, mobileScreen, threadParent, threadExiting, activeView, settingsExiting])

	const handleSwipeBack = useCallback(() => {
		if (swipeTargetLayer === 'thread') {
			handleCloseThread()
		} else if (swipeTargetLayer === 'layer1') {
			handleBackMobile()
		}
	}, [swipeTargetLayer, handleCloseThread, handleBackMobile])

	useSwipeBack({
		shellRef: chatShellRef,
		targetLayer: swipeTargetLayer,
		enabled: authState === 'authenticated',
		disabled: searchOpen || authState !== 'authenticated',
		onBack: handleSwipeBack
	})

	if (window.self !== window.top) {
		return <div className="iframe-fallback">Nuage Notify is loaded inside an iframe.</div>
	}

	if (authState === 'loading') {
		return (
			<div className="login-shell">
				<div className="loading-badge">読み込み中...</div>
			</div>
		)
	}

	if (authState === 'unauthenticated' || !user) {
		return <LoginScreen apiBase={apiBase} onLoginSuccess={loadBootstrap} />
	}

	const activeChannel = channels.find((c) => c.id === activeChannelId) || channels[0] || null
	const showSettings = activeView === 'settings' || settingsExiting
	const settingsTransitionClass = settingsExiting
		? 'pane-exiting'
		: settingsEntering
		? 'pane-entering'
		: ''
	const threadTransitionClass = threadExiting
		? 'pane-exiting'
		: threadEntering
		? 'pane-entering'
		: ''

	return (
		<div
			ref={chatShellRef}
			className={`chat-shell mobile-${mobileScreen} ${activeView === 'settings' && !settingsExiting ? 'view-settings' : ''}`.trim()}
		>
			<ChannelSidebar
				channels={channels}
				activeChannelId={activeChannel?.id || null}
				activeView={activeView}
				user={user}
				bot={bot}
				onSelectChannel={handleSelectChannel}
				onOpenSearch={() => setSearchOpen(true)}
				onOpenSettings={() => {
					const prevScreen: HistoryScreen =
						mobileScreen === 'thread' ? 'thread' : mobileScreen
					closeThreadWithAnimation(true)
					openSettingsView()
					pushHistoryRoute('/settings', 'settings', prevScreen)
				}}
				onLogout={() => void handleLogout()}
			/>

			{showSettings && (
				<section className={`settings-pane ${settingsTransitionClass}`.trim()} aria-label="設定">
					<header className="chat-header">
						<div className="chat-header-left">
							<button
								type="button"
								onClick={handleBackMobile}
								className="icon-btn"
								aria-label="チャットへ戻る"
							>
								<ChevronLeft size={20} />
							</button>
							<h2 className="chat-header-title">
								<SettingsIcon size={17} />
								設定
							</h2>
						</div>
					</header>
					<div className="settings-scroll">
						<div className="settings-inner">
							<Settings
								isSubscribed={isSubscribed}
								submitting={submitting}
								onSubscribeToggle={isSubscribed ? unsubscribe : subscribe}
								onSendTestNotify={sendTestNotify}
								logs={logs}
							/>
						</div>
					</div>
				</section>
			)}

			{!showSettings && (
				<>
					{activeChannel ? (
						<MessageList
							channel={activeChannel}
							messages={messages}
							channelsById={channelsById}
							hasMore={hasMore}
							loadingOlder={loadingOlder}
							loadingInitial={loadingMessages}
							currentUserId={user.id}
							bot={bot}
							sending={sending}
							sendError={sendError}
							onBackMobile={handleBackMobile}
							onLoadOlder={handleLoadOlder}
							onSendMessage={(text) => handlePostMessage(text)}
							onOpenThread={handleOpenThread}
							onOpenSearch={() => setSearchOpen(true)}
						/>
					) : (
						<section className="chat-main">
							<div className="empty-state">チャンネルを選択する。</div>
						</section>
					)}

					{threadParent && (
						<ThreadPane
							parent={threadParent}
							replies={threadReplies}
							loadingReplies={loadingThread}
							channelsById={channelsById}
							currentUserId={user.id}
							bot={bot}
							sending={sending}
							sendError={sendError}
							transitionClass={threadTransitionClass}
							onClose={handleCloseThread}
							onSendReply={(text) => handlePostMessage(text, threadParent.ts)}
						/>
					)}
				</>
			)}

			{searchOpen && (
				<SearchModal
					apiBase={apiBase}
					onClose={() => setSearchOpen(false)}
					onSelectResult={handleSelectSearchResult}
				/>
			)}
		</div>
	)
}
