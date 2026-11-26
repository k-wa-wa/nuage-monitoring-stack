import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { ChevronLeft, Settings as SettingsIcon } from 'lucide-react'
import LoginScreen from './components/LoginScreen'
import ChannelSidebar from './components/ChannelSidebar'
import MessageList from './components/MessageList'
import ThreadPane from './components/ThreadPane'
import SearchModal from './components/SearchModal'
import Settings from './components/Settings'
import { parseChatRoute } from './utils/url'
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

export default function App() {
	const initialRoute = useMemo(() => parseChatRoute(window.location.pathname), [])

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
	const [mobileScreen, setMobileScreen] = useState<MobileScreen>(() => {
		if (initialRoute.threadTs || initialRoute.deepLinkKind) return 'thread'
		if (initialRoute.channelId) return 'messages'
		return 'channels'
	})
	const [routePath, setRoutePath] = useState(() => window.location.pathname)

	const [messages, setMessages] = useState<ChatMessage[]>([])
	const [hasMore, setHasMore] = useState(false)
	const [loadingOlder, setLoadingOlder] = useState(false)
	const [sending, setSending] = useState(false)
	const [sendError, setSendError] = useState<string | null>(null)

	const [threadParent, setThreadParent] = useState<ChatMessage | null>(null)
	const [threadReplies, setThreadReplies] = useState<ChatMessage[]>([])
	const [searchOpen, setSearchOpen] = useState(false)

	// Push 通知設定関連
	const [isSubscribed, setIsSubscribed] = useState(false)
	const [submitting, setSubmitting] = useState(false)
	const [logs, setLogs] = useState<string[]>([])

	const activeChannelIdRef = useRef<string | null>(activeChannelId)
	const threadParentTsRef = useRef<string | null>(null)
	const threadChannelIdRef = useRef<string | null>(null)
	const userIdRef = useRef<string | null>(null)
	const resolvedRouteRef = useRef<string | null>(null)

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
				setMessages(data.messages || [])
				setHasMore(Boolean(data.has_more))
				const latestTs =
					data.messages && data.messages.length > 0
						? data.messages[data.messages.length - 1].ts
						: undefined
				void markChannelRead(channelId, latestTs)
			} catch (err) {
				console.error('Failed to load messages:', err)
			}
		},
		[apiBase, markChannelRead]
	)

	const loadThread = useCallback(
		async (channelId: string, threadTs: string) => {
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
				setThreadParent(data.parent)
				setThreadReplies(data.replies || [])
				setMobileScreen('thread')
			} catch (err) {
				console.error('Failed to load thread:', err)
			}
		},
		[apiBase]
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
			setActiveView('settings')
			resolvedRouteRef.current = routePath
			return
		}

		setActiveView('chat')

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
				void loadThread(parsed.channelId, parsed.threadTs)
			} else {
				setThreadParent(null)
				setThreadReplies([])
				setMobileScreen('messages')
			}
			resolvedRouteRef.current = routePath
			return
		}

		resolvedRouteRef.current = routePath
	}, [authState, routePath, apiBase, loadThread])

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

	const navigateToPath = useCallback((newPath: string) => {
		resolvedRouteRef.current = null
		setRoutePath(newPath)
		if (window.location.pathname !== newPath) {
			window.history.pushState(null, '', newPath)
		}
	}, [])

	// 戻る・進むボタンと Service Worker からの通知クリックを受信する
	useEffect(() => {
		const handlePopState = () => {
			resolvedRouteRef.current = null
			setRoutePath(window.location.pathname)
		}
		const handleSwMessage = (event: MessageEvent) => {
			if (event.data?.type === 'NOTIFICATION_CLICK' && typeof event.data.url === 'string') {
				navigateToPath(event.data.url)
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

	// プッシュ通知の購読状態チェック
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
		setThreadParent(null)
		setThreadReplies([])
	}

	const handleSelectChannel = (channelId: string) => {
		setActiveView('chat')
		setActiveChannelId(channelId)
		setThreadParent(null)
		setThreadReplies([])
		setSendError(null)
		setMobileScreen('messages')
		const nextPath = `/channels/${channelId}`
		resolvedRouteRef.current = nextPath
		setRoutePath(nextPath)
		if (window.location.pathname !== nextPath) {
			window.history.pushState(null, '', nextPath)
		}
	}

	const handleOpenThread = (msg: ChatMessage) => {
		setThreadParent(msg)
		setThreadReplies([])
		setMobileScreen('thread')
		const nextPath = `/channels/${msg.channel_id}/threads/${msg.ts}`
		resolvedRouteRef.current = nextPath
		setRoutePath(nextPath)
		if (window.location.pathname !== nextPath) {
			window.history.pushState(null, '', nextPath)
		}
		void loadThread(msg.channel_id, msg.ts)
	}

	const handleCloseThread = () => {
		setThreadParent(null)
		setThreadReplies([])
		setMobileScreen('messages')
		if (activeChannelId) {
			const nextPath = `/channels/${activeChannelId}`
			resolvedRouteRef.current = nextPath
			setRoutePath(nextPath)
			if (window.location.pathname !== nextPath) {
				window.history.pushState(null, '', nextPath)
			}
		}
	}

	const handleLoadOlder = async () => {
		if (!activeChannelId || loadingOlder || messages.length === 0) return
		const oldestTs = messages[0].ts
		setLoadingOlder(true)
		try {
			const res = await fetch(
				`${apiBase}/api/channels/${encodeURIComponent(activeChannelId)}/messages?limit=40&before=${encodeURIComponent(oldestTs)}`,
				{ credentials: 'include' }
			)
			if (!res.ok) return
			const data: { messages: ChatMessage[]; has_more: boolean } = await res.json()
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
		setActiveView('chat')
		setActiveChannelId(res.channel_id)
		const targetThreadTs = res.message.thread_ts || res.message.ts
		setMobileScreen('thread')
		const nextPath = `/channels/${res.channel_id}/threads/${targetThreadTs}`
		resolvedRouteRef.current = nextPath
		setRoutePath(nextPath)
		window.history.pushState(null, '', nextPath)
		void loadThread(res.channel_id, targetThreadTs)
	}

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

	return (
		<div className={`chat-shell mobile-${mobileScreen} ${activeView === 'settings' ? 'view-settings' : ''}`}>
			<ChannelSidebar
				channels={channels}
				activeChannelId={activeChannel?.id || null}
				activeView={activeView}
				user={user}
				bot={bot}
				onSelectChannel={handleSelectChannel}
				onOpenSearch={() => setSearchOpen(true)}
				onOpenSettings={() => {
					setActiveView('settings')
					setMobileScreen('messages')
					navigateToPath('/settings')
				}}
				onLogout={() => void handleLogout()}
			/>

			{activeView === 'settings' ? (
				<section className="settings-pane" aria-label="設定">
					<header className="chat-header">
						<div className="chat-header-left">
							<button
								type="button"
								onClick={() => {
									setActiveView('chat')
									setMobileScreen('channels')
									navigateToPath(activeChannel ? `/channels/${activeChannel.id}` : '/')
								}}
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
			) : (
				<>
					{activeChannel ? (
						<MessageList
							channel={activeChannel}
							messages={messages}
							channelsById={channelsById}
							hasMore={hasMore}
							loadingOlder={loadingOlder}
							currentUserId={user.id}
							bot={bot}
							sending={sending}
							sendError={sendError}
							onBackMobile={() => setMobileScreen('channels')}
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
							channelsById={channelsById}
							currentUserId={user.id}
							bot={bot}
							sending={sending}
							sendError={sendError}
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
