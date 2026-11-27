import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore, type ReactNode } from 'react'
import { fetchBootstrap, logoutSession } from '../api/auth'
import {
	createChatEventSource,
	fetchChannelMessages,
	fetchThread,
	markChannelRead,
	postChatMessage,
	type PostMessageResponse
} from '../api/chat'
import type { ChatMessage } from '../types/chat'
import {
	ChatContext,
	ChatStore,
	DEFAULT_BOT,
	threadKey,
	type ChatContextValue,
	type PostResult
} from './chatStore'

function appendUnique(list: ChatMessage[], msg: ChatMessage): ChatMessage[] {
	return list.some((m) => m.ts === msg.ts) ? list : [...list, msg]
}

export function ChatProvider({ children }: { children: ReactNode }) {
	const store = useMemo(
		() =>
			new ChatStore({
				authState: 'loading',
				user: null,
				bot: DEFAULT_BOT,
				channels: [],
				bootstrapLoaded: false,
				channelData: {},
				threadData: {},
				activityVersion: 0
			}),
		[]
	)
	const setAuthState = useMemo(() => store.setter('authState'), [store])
	const setUser = useMemo(() => store.setter('user'), [store])
	const setBot = useMemo(() => store.setter('bot'), [store])
	const setChannels = useMemo(() => store.setter('channels'), [store])
	const setBootstrapLoaded = useMemo(() => store.setter('bootstrapLoaded'), [store])
	const setChannelData = useMemo(() => store.setter('channelData'), [store])
	const setThreadData = useMemo(() => store.setter('threadData'), [store])
	const setActivityVersion = useMemo(() => store.setter('activityVersion'), [store])
	const authState = useSyncExternalStore(store.subscribe, () => store.get().authState)
	const user = useSyncExternalStore(store.subscribe, () => store.get().user)

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
		setBootstrapLoaded(false)
	}, [setAuthState, setBootstrapLoaded, setUser])

	const reloadBootstrap = useCallback(async () => {
		try {
			const data = await fetchBootstrap(handleUnauthorized)
			if (!data) return
			setUser(data.user)
			setChannels(data.channels || [])
			setBot(data.bot)
			setBootstrapLoaded(true)
			setAuthState('authenticated')
		} catch (err) {
			console.error('Bootstrap failed:', err)
			setAuthState((prev) => (prev === 'loading' ? 'unauthenticated' : prev))
		}
	}, [handleUnauthorized, setAuthState, setBot, setBootstrapLoaded, setChannels, setUser])

	useEffect(() => {
		void reloadBootstrap()
	}, [reloadBootstrap])

	const markRead = useCallback(
		async (channelId: string, ts?: string) => {
			setChannels((prev) =>
				prev.map((ch) => (ch.id === channelId ? { ...ch, unread_count: 0 } : ch))
			)
			try {
				await markChannelRead(channelId, ts, handleUnauthorized)
			} catch {
				// 既読の記録に失敗しても表示は続ける (次の bootstrap で未読数が戻る)
			}
		},
		[handleUnauthorized, setChannels]
	)

	const loadChannel = useCallback(
		async (channelId: string) => {
			setChannelData((prev) => {
				const cur = prev[channelId]
				return {
					...prev,
					[channelId]: {
						messages: cur?.messages ?? [],
						hasMore: cur?.hasMore ?? false,
						loaded: cur?.loaded ?? false,
						loading: true,
						loadingOlder: cur?.loadingOlder ?? false,
						error: null
					}
				}
			})
			try {
				const data = await fetchChannelMessages(channelId, { limit: 40 }, handleUnauthorized)
				if (!data) return
				const messages = data.messages || []
				setChannelData((prev) => ({
					...prev,
					[channelId]: {
						messages,
						hasMore: Boolean(data.has_more),
						loaded: true,
						loading: false,
						loadingOlder: false,
						error: null
					}
				}))
				if (viewingRef.current.get(channelId)) {
					void markRead(channelId, messages[messages.length - 1]?.ts)
				}
			} catch (err) {
				console.error('Failed to load messages:', err)
				setChannelData((prev) => {
					const cur = prev[channelId]
					return {
						...prev,
						[channelId]: {
							messages: cur?.messages ?? [],
							hasMore: cur?.hasMore ?? false,
							loaded: cur?.loaded ?? false,
							loading: false,
							loadingOlder: false,
							error: 'メッセージを読み込めなかった。'
						}
					}
				})
			}
		},
		[handleUnauthorized, markRead, setChannelData]
	)

	const loadOlder = useCallback(
		async (channelId: string) => {
			const current = store.get().channelData[channelId]
			if (!current || current.loadingOlder || !current.hasMore || current.messages.length === 0) {
				return
			}
			const oldestTs = current.messages[0].ts
			setChannelData((prev) => ({
				...prev,
				[channelId]: { ...prev[channelId], loadingOlder: true }
			}))
			try {
				const data = await fetchChannelMessages(channelId, { limit: 40, before: oldestTs }, handleUnauthorized)
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
		[handleUnauthorized, store, setChannelData]
	)

	const loadThread = useCallback(
		async (channelId: string, threadTs: string) => {
			const key = threadKey(channelId, threadTs)
			setThreadData((prev) => {
				const cur = prev[key]
				return {
					...prev,
					[key]: {
						parent: cur?.parent ?? null,
						replies: cur?.replies ?? [],
						loaded: cur?.loaded ?? false,
						loading: true,
						error: null
					}
				}
			})
			try {
				const data = await fetchThread(channelId, threadTs, handleUnauthorized)
				if (!data) return
				if (data.notFound) {
					setThreadData((prev) => ({
						...prev,
						[key]: { parent: null, replies: [], loaded: true, loading: false, error: null }
					}))
					return
				}
				loadedThreadsRef.current.add(key)
				setThreadData((prev) => ({
					...prev,
					[key]: {
						parent: data.parent,
						replies: data.replies || [],
						loaded: true,
						loading: false,
						error: null
					}
				}))
			} catch (err) {
				console.error('Failed to load thread:', err)
				setThreadData((prev) => {
					const cur = prev[key]
					return {
						...prev,
						[key]: {
							parent: cur?.parent ?? null,
							replies: cur?.replies ?? [],
							loaded: cur?.loaded ?? false,
							loading: false,
							error: 'スレッドを読み込めなかった。'
						}
					}
				})
			}
		},
		[handleUnauthorized, setThreadData]
	)

	const postMessage = useCallback(
		async (channelId: string, text: string, threadTs?: string): Promise<PostResult> => {
			try {
				const res = await postChatMessage(channelId, text, threadTs, handleUnauthorized)
				if (!res) return { ok: false, error: 'ログインが切れた。もう一度ログインしてほしい。' }
				if (!res.ok) return { ok: false, error: `送信に失敗した (HTTP ${res.status})。` }
				const data: PostMessageResponse = await res.json()
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
		[handleUnauthorized, setBot, setChannelData, setThreadData]
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
			await logoutSession()
		} finally {
			setChannelData({})
			setThreadData({})
			setChannels([])
			loadedThreadsRef.current.clear()
			handleUnauthorized()
		}
	}, [handleUnauthorized, setChannelData, setChannels, setThreadData])

	// 再接続・復帰時に取りこぼしを埋めるため、表示中のものを読み直す
	const resyncRef = useRef<() => void>(() => {})
	useEffect(() => {
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
	})

	// SSE でリアルタイム更新を受ける
	useEffect(() => {
		if (authState !== 'authenticated') return
		const es = createChatEventSource()
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
	}, [authState, markRead, setBot, setChannels, setChannelData, setThreadData, setActivityVersion])

	const value = useMemo<ChatContextValue>(
		() => ({
			store,
			actions: { reloadBootstrap, logout, loadChannel, loadOlder, loadThread, postMessage, enterChannel }
		}),
		[store, reloadBootstrap, logout, loadChannel, loadOlder, loadThread, postMessage, enterChannel]
	)

	return <ChatContext.Provider value={value}>{children}</ChatContext.Provider>
}
