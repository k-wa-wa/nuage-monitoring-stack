import { afterAll, afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { JSDOM } from 'jsdom'
import { createElement, type ReactNode } from 'react'
import { getServiceWorkerRegistration } from '../src/api/push'
import { useActivity } from '../src/hooks/useActivity'
import { useChannelSearch } from '../src/hooks/useChannelSearch'
import { usePushSettings } from '../src/hooks/usePushSettings'
import { ChatProvider } from '../src/state/ChatContext'
import { ChatContext, ChatStore, DEFAULT_BOT, useChatActions, useChatState, type ChatActions } from '../src/state/chatStore'

const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'http://localhost/' })
const savedGlobals: Partial<Record<'window' | 'document' | 'navigator' | 'HTMLElement' | 'Node', unknown>> = {
	window: (globalThis as Record<string, unknown>).window,
	document: (globalThis as Record<string, unknown>).document,
	navigator: (globalThis as Record<string, unknown>).navigator,
	HTMLElement: (globalThis as Record<string, unknown>).HTMLElement,
	Node: (globalThis as Record<string, unknown>).Node
}
const savedFetch = globalThis.fetch

Object.assign(globalThis, {
	window: dom.window,
	document: dom.window.document,
	navigator: dom.window.navigator,
	HTMLElement: dom.window.HTMLElement,
	Node: dom.window.Node
})

const { act, cleanup, renderHook, waitFor } = await import('@testing-library/react')

afterAll(() => {
	globalThis.fetch = savedFetch
	for (const [k, v] of Object.entries(savedGlobals)) {
		if (v === undefined) delete (globalThis as Record<string, unknown>)[k]
		else (globalThis as Record<string, unknown>)[k] = v
	}
	dom.window.close()
})

afterEach(() => {
	cleanup()
	globalThis.fetch = savedFetch
	delete (dom.window as unknown as Record<string, unknown>).PushManager
})

function createMockStore(activityVersion = 0) {
	return new ChatStore({
		authState: 'authenticated',
		user: { id: 'U1', name: 'admin', display_name: 'Admin' },
		bot: DEFAULT_BOT,
		channels: [],
		bootstrapLoaded: true,
		channelData: {},
		threadData: {},
		activityVersion
	})
}

const NOOP_ACTIONS: ChatActions = {
	reloadBootstrap: async () => {},
	logout: async () => {},
	loadChannel: async () => {},
	loadOlder: async () => {},
	loadThread: async () => {},
	postMessage: async () => ({ ok: true }),
	enterChannel: () => () => {}
}

function makeWrapper(store: ChatStore) {
	return function Wrapper({ children }: { children: ReactNode }) {
		return createElement(ChatContext.Provider, { value: { store, actions: NOOP_ACTIONS } }, children)
	}
}

describe('useChannelSearch', () => {
	test('文字入力直後から searching=true になり、デバウンス後に最後のクエリだけ検索し、空でリセットする', async () => {
		const requestedUrls: string[] = []
		globalThis.fetch = (async (input: RequestInfo | URL) => {
			requestedUrls.push(String(input))
			return new Response(
				JSON.stringify([
					{
						channel_id: 'C1',
						channel_name: 'general',
						is_im: false,
						message: {
							channel_id: 'C1',
							ts: '1.0',
							username: 'bot',
							text: 'alert found',
							created_at: '2026-11-27T00:00:00Z',
							updated_at: '2026-11-27T00:00:00Z'
						}
					}
				]),
				{ status: 200 }
			)
		}) as unknown as typeof fetch

		const { result } = renderHook(() => useChannelSearch())
		expect(result.current.searching).toBe(false)
		expect(result.current.results).toBeNull()

		act(() => {
			result.current.setQuery('a')
		})
		// デバウンス中も直ちに検索中状態になる
		expect(result.current.searching).toBe(true)
		expect(requestedUrls).toEqual([])

		act(() => {
			result.current.setQuery('alert')
		})

		await waitFor(() => {
			expect(result.current.searching).toBe(false)
			expect(result.current.results).toHaveLength(1)
		})
		expect(requestedUrls).toEqual(['/api/search?q=alert'])

		act(() => {
			result.current.setQuery('   ')
		})
		expect(result.current.searching).toBe(false)
		expect(result.current.results).toBeNull()
		expect(result.current.error).toBeNull()
	})

	test('進行中の検索があるときに次の検索が始まると前のリクエストを abort する', async () => {
		const signals: AbortSignal[] = []
		let callCount = 0
		globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
			callCount += 1
			if (init?.signal) signals.push(init.signal)
			if (callCount === 1) {
				return await new Promise<Response>((_resolve, reject) => {
					init?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
				})
			}
			return new Response(JSON.stringify([]), { status: 200 })
		}) as unknown as typeof fetch

		const { result } = renderHook(() => useChannelSearch())
		act(() => {
			result.current.setQuery('first')
		})
		await waitFor(() => expect(signals).toHaveLength(1))
		expect(signals[0].aborted).toBe(false)

		act(() => {
			result.current.setQuery('second')
		})
		expect(signals[0].aborted).toBe(true)
		expect(result.current.searching).toBe(true)

		await waitFor(() => expect(result.current.searching).toBe(false))
		expect(result.current.results).toEqual([])
		expect(result.current.error).toBeNull()
	})

	test('検索 API が失敗したときはエラーメッセージを設定し searching を解除する', async () => {
		globalThis.fetch = (async () => new Response('fail', { status: 500 })) as unknown as typeof fetch

		const { result } = renderHook(() => useChannelSearch())
		act(() => {
			result.current.setQuery('boom')
		})

		await waitFor(() => expect(result.current.searching).toBe(false))
		expect(result.current.error).toBe('検索できなかった。')
	})
})

describe('useActivity', () => {
	test('取得失敗時に items を空配列にせず error を保持し、reload で回復する', async () => {
		let attempt = 0
		const requestedUrls: string[] = []
		globalThis.fetch = (async (input: RequestInfo | URL) => {
			requestedUrls.push(String(input))
			attempt += 1
			if (attempt === 1) {
				return new Response('err', { status: 500 })
			}
			return new Response(
				JSON.stringify({
					items: [
						{
							channel_id: 'C1',
							channel_name: 'alerts',
							message: {
								channel_id: 'C1',
								ts: '1.0',
								username: 'Alertmanager',
								text: 'KubeJobFailed',
								created_at: '2026-11-27T00:00:00Z',
								updated_at: '2026-11-27T00:00:00Z'
							}
						}
					],
					has_more: false,
					firing_count: 2
				}),
				{ status: 200 }
			)
		}) as unknown as typeof fetch

		const store = createMockStore()
		const { result } = renderHook(() => useActivity(), { wrapper: makeWrapper(store) })

		await waitFor(() => expect(result.current.loading).toBe(false))
		expect(result.current.error).toBe('通知を読み込めなかった。')
		expect(result.current.items).toBeNull()

		act(() => {
			result.current.reload()
		})
		expect(result.current.loading).toBe(true)

		await waitFor(() => expect(result.current.loading).toBe(false))
		expect(result.current.error).toBeNull()
		expect(result.current.items).toHaveLength(1)
		expect(result.current.firingCount).toBe(2)

		act(() => {
			result.current.setFilter('firing')
		})
		expect(result.current.filter).toBe('firing')
		expect(result.current.items).toBeNull()
		expect(result.current.loading).toBe(true)
		await waitFor(() => expect(result.current.loading).toBe(false))
		expect(requestedUrls[requestedUrls.length - 1]).toContain('filter=firing')
	})
})

describe('getServiceWorkerRegistration と usePushSettings', () => {
	beforeEach(() => {
		;(dom.window as unknown as Record<string, unknown>).PushManager = function PushManager() {}
	})

	test('SW 未登録かつ ready が解決しない場合、タイムアウトで null を返す', async () => {
		Object.defineProperty(dom.window.navigator, 'serviceWorker', {
			configurable: true,
			value: {
				getRegistration: async () => undefined,
				ready: new Promise(() => {})
			}
		})

		const reg = await getServiceWorkerRegistration(25)
		expect(reg).toBeNull()
	})

	test('usePushSettings は /api/subscribe の同期完了を待たずに checking を終えて subscribed を確定する', async () => {
		const fakeSub = { endpoint: 'https://push.example/1', unsubscribe: async () => true }
		Object.defineProperty(dom.window.navigator, 'serviceWorker', {
			configurable: true,
			value: {
				getRegistration: async () => ({
					pushManager: {
						getSubscription: async () => fakeSub
					}
				}),
				ready: Promise.resolve({})
			}
		})

		let resolveSync!: (res: Response) => void
		let syncCalled = false
		globalThis.fetch = (async () => {
			syncCalled = true
			return await new Promise<Response>((resolve) => {
				resolveSync = resolve
			})
		}) as unknown as typeof fetch

		const toasts: Array<{ msg: string; tone?: string }> = []
		const { result } = renderHook(() => usePushSettings((msg, tone) => toasts.push({ msg, tone })))

		expect(result.current.checking).toBe(true)
		await waitFor(() => expect(result.current.checking).toBe(false))
		expect(result.current.subscribed).toBe(true)
		expect(syncCalled).toBe(true)

		// 同期が保留中でも、既に確定済みなのでテスト通知送信は実行できる
		resolveSync(new Response(null, { status: 201 }))
	})

	test('usePushSettings の togglePush(false) 実行中は busy=true になり、完了後に subscribed=false になる', async () => {
		let unsubscribed = false
		const fakeSub = {
			endpoint: 'https://push.example/1',
			unsubscribe: async () => {
				unsubscribed = true
				return true
			}
		}
		Object.defineProperty(dom.window.navigator, 'serviceWorker', {
			configurable: true,
			value: {
				getRegistration: async () => ({
					pushManager: {
						getSubscription: async () => (unsubscribed ? null : fakeSub)
					}
				}),
				ready: Promise.resolve({
					pushManager: {
						getSubscription: async () => (unsubscribed ? null : fakeSub)
					}
				})
			}
		})

		let resolveUnsubscribe!: (res: Response) => void
		globalThis.fetch = (async (input: RequestInfo | URL) => {
			const url = String(input)
			if (url.endsWith('/api/subscribe')) return new Response(null, { status: 201 })
			return await new Promise<Response>((resolve) => {
				resolveUnsubscribe = resolve
			})
		}) as unknown as typeof fetch

		const toasts: Array<{ msg: string; tone?: string }> = []
		const { result } = renderHook(() => usePushSettings((msg, tone) => toasts.push({ msg, tone })))
		await waitFor(() => expect(result.current.checking).toBe(false))

		let promise!: Promise<void>
		act(() => {
			promise = result.current.togglePush(false)
		})
		await waitFor(() => expect(result.current.busy).toBe(true))

		resolveUnsubscribe(new Response(null, { status: 200 }))
		await act(async () => {
			await promise
		})
		expect(result.current.busy).toBe(false)
		expect(result.current.subscribed).toBe(false)
		expect(toasts.some((t) => t.msg === '通知の登録を解除した。')).toBe(true)
	})
})

describe('ChatContext のロード中 SSE 割り込み防止', () => {
	test('loadChannel 完了前の SSE message_created は未ロードの channelData に割り込まず、失敗時のエラー表示を隠さない', async () => {
		const sseListeners = new Map<string, Array<(e: MessageEvent) => void>>()
		const savedES = (globalThis as Record<string, unknown>).EventSource
		;(globalThis as Record<string, unknown>).EventSource = class MockES {
			addEventListener(type: string, fn: (e: MessageEvent) => void) {
				const list = sseListeners.get(type) ?? []
				list.push(fn)
				sseListeners.set(type, list)
			}
			close() {}
		}

		let resolveMsgs!: (res: Response) => void
		globalThis.fetch = (async (input: RequestInfo | URL) => {
			const url = String(input)
			if (url.endsWith('/api/bootstrap')) {
				return new Response(
					JSON.stringify({
						user: { id: 'U1', name: 'admin', display_name: 'Admin' },
						bot: DEFAULT_BOT,
						channels: []
					}),
					{ status: 200 }
				)
			}
			if (url.includes('/messages')) {
				return await new Promise<Response>((resolve) => {
					resolveMsgs = resolve
				})
			}
			return new Response('{}', { status: 200 })
		}) as unknown as typeof fetch

		try {
			const { result } = renderHook(
				() => ({
					authState: useChatState((s) => s.authState),
					chData: useChatState((s) => s.channelData.C1),
					actions: useChatActions()
				}),
				{
					wrapper: ({ children }: { children: ReactNode }) => createElement(ChatProvider, null, children)
				}
			)

			await waitFor(() => expect(result.current.authState).toBe('authenticated'))

			let loadPromise!: Promise<void>
			act(() => {
				loadPromise = result.current.actions.loadChannel('C1')
			})
			await waitFor(() => expect(result.current.chData?.loading).toBe(true))

			// 読み込み中に SSE message_created が届いても loaded=false の間は messages に追加されない
			act(() => {
				for (const fn of sseListeners.get('message_created') ?? []) {
					fn(
						new MessageEvent('message_created', {
							data: JSON.stringify({
								payload: {
									channel_id: 'C1',
									ts: '100.0',
									username: 'bot',
									text: 'incoming',
									created_at: '2026-11-27T00:00:00Z',
									updated_at: '2026-11-27T00:00:00Z'
								}
							})
						})
					)
				}
			})
			expect(result.current.chData?.messages).toEqual([])
			expect(result.current.chData?.loaded).toBe(false)

			// 取得失敗しても messages は空のまま error が保持される
			resolveMsgs(new Response('err', { status: 500 }))
			await act(async () => {
				await loadPromise
			})
			expect(result.current.chData?.loaded).toBe(false)
			expect(result.current.chData?.error).toBe('メッセージを読み込めなかった。')
			expect(result.current.chData?.messages).toEqual([])
		} finally {
			if (savedES === undefined) delete (globalThis as Record<string, unknown>).EventSource
			else (globalThis as Record<string, unknown>).EventSource = savedES
		}
	})
})
