import { describe, expect, test } from 'bun:test'
import { syncExistingPushSubscription } from '../src/api/push'

describe('syncExistingPushSubscription', () => {
	test('ログイン後の既存 Push 購読を /api/subscribe へ再送信する', async () => {
		const calls: Array<{ url: string; init?: RequestInit }> = []
		const mockFetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
			calls.push({ url: String(input), init })
			return new Response(null, { status: 201 })
		}) as unknown as typeof fetch

		const logs: string[] = []
		const fakeSub = {
			endpoint: 'https://push.example.com/sub/123',
			keys: { p256dh: 'pub', auth: 'auth' }
		}

		const ok = await syncExistingPushSubscription(
			'http://localhost:8080',
			fakeSub,
			(m) => logs.push(m),
			mockFetch
		)

		expect(ok).toBe(true)
		expect(logs).toEqual([])
		expect(calls).toHaveLength(1)
		expect(calls[0].url).toBe('http://localhost:8080/api/subscribe')
		expect(calls[0].init?.method).toBe('POST')
		expect(calls[0].init?.credentials).toBe('include')
		expect(calls[0].init?.body).toBe(JSON.stringify(fakeSub))
	})

	test('サーバーがエラーを返した場合は false を返しログを記録する', async () => {
		const mockFetch = (async () => {
			return new Response('unauthorized', { status: 401 })
		}) as unknown as typeof fetch

		const logs: string[] = []
		const ok = await syncExistingPushSubscription(
			'',
			{ endpoint: 'https://push.example.com/sub/123', keys: { p256dh: 'pub', auth: 'auth' } },
			(m) => logs.push(m),
			mockFetch
		)

		expect(ok).toBe(false)
		expect(logs).toEqual(['購読情報の同期に失敗した: HTTP 401'])
	})
})
