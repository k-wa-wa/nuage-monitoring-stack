import {
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
	type ReactNode
} from 'react'
import type { AuthUser, BootstrapResponse, BotInfo, Channel, ChatMessage } from '../types/chat'
import {
	API_BASE,
	ChatContext,
	DEFAULT_BOT,
	threadKey,
	type AuthState,
	type ChannelData,
	type ChatContextValue,
	type PostResult,
	type ThreadData
} from './chatStore'

function appendUnique(list: ChatMessage[], msg: ChatMessage): ChatMessage[] {
	return list.some((m) => m.ts === msg.ts) ? list : [...list, msg]
}

export function ChatProvider({ children }: { children: ReactNode }) {
	const [authState, setAuthState] = useState<AuthState>('loading')
	const [user, setUser] = useState<AuthUser | null>(null)
	const [bot, setBot] = useState<BotInfo>(DEFAULT_BOT)
	const [channels, setChannels] = useState<Channel[]>([])
	const [channelData, setChannelData] = useState<Record<string, ChannelData>>({})
	const [threadData, setThreadData] = useState<Record<string, ThreadData>>({})
	const [activityVersion, setActivityVersion] = useState(0)

	const userIdRef = useRef<string | null>(null)
	// チャンネル ID ごとの表示中の画面数 (チャンネル画面とスレッド画面が重なることがある)
	const viewingRef = useRef<Map<string, number>>(new Map())
	const loadedThreadsRef = useRef<Set<string>>(new Set())

	useEffect(() => {
		userIdRef.current = user?.id ?? null
	}, [user])

	const handleUnauthorized = useCallback(() => {
		setAuthState('unauthenticated')
		setUser(null)
	}, [])

	const apiFetch = useCallback(
		async (path: string, init?: RequestInit): Promise<Response | null> => {
			const res = await fetch(`${API_BASE}${path}`, { credentials: 'include', ...init })
			if (res.status === 401) {
				handleUnauthorized()
				return null
			}
			return res
		},
		[handleUnauthorized]
	)

	const reloadBootstrap = useCallback(async () => {
		try {
			const res = await apiFetch('/api/bootstrap')
			if (!res) return
			if (!res.ok) {
				setAuthState('unauthenticated')
				return
			}
			const data: BootstrapResponse = await res.json()
			setUser(data.user)
			setChannels(data.channels || [])
			setBot(data.bot)
			setAuthState('authenticated')
		} catch (err) {
			console.error('Bootstrap failed:', err)
			setAuthState((prev) => (prev === 'loading' ? 'unauthenticated' : prev))
		}
	}, [apiFetch])

	useEffect(() => {
		void reloadBootstrap()
	}, [reloadBootstrap])

	const markRead = useCallback(
		async (channelId: string, ts?: string) => {
			setChannels((prev) =>
				prev.map((ch) => (ch.id === channelId ? { ...ch, unread_count: 0 } : ch))
			)
			try {
				await apiFetch(`/api/channels/${encodeURIComponent(channelId)}/read`, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ ts: ts || '' })
				})
			} catch {
				// 既読の記録に失敗しても表示は続ける (次の bootstrap で未読数が戻る)
			}
		},
		[apiFetch]
	)

	const loadChannel = useCallback(
		async (channelId: string) => {
			try {
				const res = await apiFetch(
					`/api/channels/${encodeURIComponent(channelId)}/messages?limit=40`
				)
				if (!res || !res.ok) return
				const data: { messages: ChatMessage[]; has_more: boolean } = await res.json()
				const messages = data.messages || []
				setChannelData((prev) => ({
					...prev,
					[channelId]: {
						messages,
						hasMore: Boolean(data.has_more),
						loaded: true,
						loadingOlder: false
					}
				}))
				if (viewingRef.current.get(channelId)) {
					void markRead(channelId, messages[messages.length - 1]?.ts)
				}
			} catch (err) {
				console.error('Failed to load messages:', err)
			}
		},
		[apiFetch, markRead]
	)

	const loadOlder = useCallback(
		async (channelId: string) => {
			const current = channelData[channelId]
			if (!current || current.loadingOlder || !current.hasMore || current.messages.length === 0) {
				return
			}
			const oldestTs = current.messages[0].ts
			setChannelData((prev) => ({
				...prev,
				[channelId]: { ...prev[channelId], loadingOlder: true }
			}))
			try {
				const res = await apiFetch(
					`/api/channels/${encodeURIComponent(channelId)}/messages?limit=40&before=${encodeURIComponent(oldestTs)}`
				)
				const data: { messages: ChatMessage[]; has_more: boolean } | null =
					res && res.ok ? await res.json() : null
				setChannelData((prev) => {
					const cur = prev[channelId]
					if (!cur) return prev
					if (!data) return { ...prev, [channelId]: { ...cur, loadingOlder: false } }
					const existing = new Set(cur.messages.map((m) => m.ts))
					const older = (data.messages || []).filter((m) => !existing.has(m.ts))
					return {
						...prev,
						[channelId]: {
							...cur,
							messages: [...older, ...cur.messages],
							hasMore: Boolean(data.has_more),
							loadingOlder: false
						}
					}
				})
			} catch (err) {
				console.error('Failed to load older messages:', err)
				setChannelData((prev) =>
					prev[channelId]
						? { ...prev, [channelId]: { ...prev[channelId], loadingOlder: false } }
						: prev
				)
			}
		},
		[apiFetch, channelData]
	)

	const loadThread = useCallback(
		async (channelId: string, threadTs: string) => {
			const key = threadKey(channelId, threadTs)
			try {
				const res = await apiFetch(
					`/api/channels/${encodeURIComponent(channelId)}/threads/${encodeURIComponent(threadTs)}`
				)
				if (!res || !res.ok) {
					setThreadData((prev) => ({
						...prev,
						[key]: { parent: prev[key]?.parent ?? null, replies: prev[key]?.replies ?? [], loaded: true }
					}))
					return
				}
				const data: { parent: ChatMessage; replies: ChatMessage[] } = await res.json()
				loadedThreadsRef.current.add(key)
				setThreadData((prev) => ({
					...prev,
					[key]: { parent: data.parent, replies: data.replies || [], loaded: true }
				}))
			} catch (err) {
				console.error('Failed to load thread:', err)
			}
		},
		[apiFetch]
	)

	const postMessage = useCallback(
		async (channelId: string, text: string, threadTs?: string): Promise<PostResult> => {
			try {
				const res = await apiFetch(`/api/channels/${encodeURIComponent(channelId)}/messages`, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ text, thread_ts: threadTs || '' })
				})
				if (!res) return { ok: false, error: 'ログインが切れた。もう一度ログインしてほしい。' }
				if (!res.ok) return { ok: false, error: `送信に失敗した (HTTP ${res.status})。` }
				const data: { message: ChatMessage; bot_online: boolean } = await res.json()
				setBot((prev) => ({ ...prev, online: data.bot_online }))
				const created = data.message
				if (!created.thread_ts) {
					setChannelData((prev) =>
						prev[channelId]
							? {
									...prev,
									[channelId]: {
										...prev[channelId],
										messages: appendUnique(prev[channelId].messages, created)
									}
								}
							: prev
					)
				} else {
					const key = threadKey(channelId, created.thread_ts)
					setThreadData((prev) =>
						prev[key]
							? { ...prev, [key]: { ...prev[key], replies: appendUnique(prev[key].replies, created) } }
							: prev
					)
				}
				return { ok: true }
			} catch {
				return { ok: false, error: 'ネットワークエラーで送信できなかった。接続を確かめて再送してほしい。' }
			}
		},
		[apiFetch]
	)

	const enterChannel = useCallback(
		(channelId: string) => {
			const map = viewingRef.current
			map.set(channelId, (map.get(channelId) ?? 0) + 1)
			void markRead(channelId)
			return () => {
				const n = (map.get(channelId) ?? 1) - 1
				if (n <= 0) map.delete(channelId)
				else map.set(channelId, n)
			}
		},
		[markRead]
	)

	const logout = useCallback(async () => {
		try {
			await fetch(`${API_BASE}/api/auth/logout`, { method: 'POST', credentials: 'include' })
		} finally {
			setChannelData({})
			setThreadData({})
			loadedThreadsRef.current.clear()
			handleUnauthorized()
		}
	}, [handleUnauthorized])

	// 再接続・復帰時に取りこぼしを埋めるため、表示中のものを読み直す
	const resyncRef = useRef<() => void>(() => {})
	resyncRef.current = () => {
		void reloadBootstrap()
		for (const channelId of viewingRef.current.keys()) {
			void loadChannel(channelId)
		}
		for (const key of loadedThreadsRef.current) {
			const idx = key.indexOf(':')
			void loadThread(key.slice(0, idx), key.slice(idx + 1))
		}
		setActivityVersion((v) => v + 1)
	}

	// SSE でリアルタイム更新を受ける
	useEffect(() => {
		if (authState !== 'authenticated') return
		const es = new EventSource(`${API_BASE}/api/events`, { withCredentials: true })
		let openedOnce = false

		es.addEventListener('open', () => {
			if (!openedOnce) {
				openedOnce = true
				return
			}
			resyncRef.current()
		})

		es.addEventListener('bot_status', (event: MessageEvent) => {
			try {
				const parsed = JSON.parse(event.data) as { payload?: { online?: boolean } }
				setBot((prev) => ({ ...prev, online: Boolean(parsed.payload?.online) }))
			} catch {
				// 壊れたイベントは捨てる
			}
		})

		es.addEventListener('message_created', (event: MessageEvent) => {
			let msg: ChatMessage | undefined
			try {
				msg = (JSON.parse(event.data) as { payload?: ChatMessage }).payload
			} catch {
				return
			}
			if (!msg) return
			const created = msg
			const viewing = Boolean(viewingRef.current.get(created.channel_id))
			const fromOthers = Boolean(created.bot_id) || created.user_id !== userIdRef.current

			if (created.source_kind) setActivityVersion((v) => v + 1)

			if (!created.thread_ts) {
				setChannelData((prev) =>
					prev[created.channel_id]
						? {
								...prev,
								[created.channel_id]: {
									...prev[created.channel_id],
									messages: appendUnique(prev[created.channel_id].messages, created)
								}
							}
						: prev
				)
			} else {
				setChannelData((prev) => {
					const cur = prev[created.channel_id]
					if (!cur) return prev
					return {
						...prev,
						[created.channel_id]: {
							...cur,
							messages: cur.messages.map((m) =>
								m.ts === created.thread_ts
									? { ...m, reply_count: (m.reply_count || 0) + 1, latest_reply: created.ts }
									: m
							)
						}
					}
				})
				const key = threadKey(created.channel_id, created.thread_ts)
				setThreadData((prev) =>
					prev[key]
						? { ...prev, [key]: { ...prev[key], replies: appendUnique(prev[key].replies, created) } }
						: prev
				)
			}

			if (viewing) {
				void markRead(created.channel_id, created.ts)
			} else if (fromOthers) {
				setChannels((prev) =>
					prev.map((ch) =>
						ch.id === created.channel_id
							? { ...ch, unread_count: ch.unread_count + 1, latest_ts: created.ts }
							: ch
					)
				)
			}
		})

		es.addEventListener('message_updated', (event: MessageEvent) => {
			let updated: ChatMessage | undefined
			try {
				updated = (JSON.parse(event.data) as { payload?: ChatMessage }).payload
			} catch {
				return
			}
			if (!updated) return
			const u = updated
			if (u.source_kind) setActivityVersion((v) => v + 1)
			setChannelData((prev) => {
				const cur = prev[u.channel_id]
				if (!cur) return prev
				return {
					...prev,
					[u.channel_id]: { ...cur, messages: cur.messages.map((m) => (m.ts === u.ts ? u : m)) }
				}
			})
			const key = threadKey(u.channel_id, u.ts)
			setThreadData((prev) => (prev[key] ? { ...prev, [key]: { ...prev[key], parent: u } } : prev))
		})

		const handleVisible = () => {
			if (document.visibilityState === 'visible') resyncRef.current()
		}
		const handleOnline = () => resyncRef.current()
		document.addEventListener('visibilitychange', handleVisible)
		window.addEventListener('online', handleOnline)

		return () => {
			document.removeEventListener('visibilitychange', handleVisible)
			window.removeEventListener('online', handleOnline)
			es.close()
		}
	}, [authState, markRead])

	const value = useMemo<ChatContextValue>(
		() => ({
			authState,
			user,
			bot,
			channels,
			channelData,
			threadData,
			activityVersion,
			reloadBootstrap,
			logout,
			loadChannel,
			loadOlder,
			loadThread,
			postMessage,
			enterChannel
		}),
		[
			authState,
			user,
			bot,
			channels,
			channelData,
			threadData,
			activityVersion,
			reloadBootstrap,
			logout,
			loadChannel,
			loadOlder,
			loadThread,
			postMessage,
			enterChannel
		]
	)

	return <ChatContext.Provider value={value}>{children}</ChatContext.Provider>
}
