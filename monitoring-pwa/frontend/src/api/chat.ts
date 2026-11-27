import type { ChatMessage, SearchResult } from '../types/chat'
import { API_BASE, ApiError, apiFetch, type UnauthorizedHandler } from './client'

export interface ChannelMessagesResponse {
	messages: ChatMessage[]
	has_more: boolean
}

export interface ThreadResponse {
	parent: ChatMessage | null
	replies: ChatMessage[]
	notFound?: boolean
}

export interface PostMessageResponse {
	message: ChatMessage
	bot_online: boolean
}

/** チャンネルのメッセージ履歴を取得する。 */
export async function fetchChannelMessages(
	channelId: string,
	opts: { limit?: number; before?: string } = {},
	onUnauthorized?: UnauthorizedHandler
): Promise<ChannelMessagesResponse | null> {
	const params = new URLSearchParams()
	params.set('limit', String(opts.limit ?? 40))
	if (opts.before) params.set('before', opts.before)
	const res = await apiFetch(
		`/api/channels/${encodeURIComponent(channelId)}/messages?${params.toString()}`,
		undefined,
		onUnauthorized
	)
	if (!res) return null
	if (!res.ok) throw new ApiError(res.status)
	return (await res.json()) as ChannelMessagesResponse
}

/** チャンネルの既読位置を更新する。 */
export async function markChannelRead(
	channelId: string,
	ts?: string,
	onUnauthorized?: UnauthorizedHandler
): Promise<void> {
	await apiFetch(
		`/api/channels/${encodeURIComponent(channelId)}/read`,
		{
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ ts: ts || '' })
		},
		onUnauthorized
	)
}

/** スレッドの親メッセージと返信一覧を取得する。404 のときは notFound: true を返す。 */
export async function fetchThread(
	channelId: string,
	threadTs: string,
	onUnauthorized?: UnauthorizedHandler
): Promise<ThreadResponse | null> {
	const res = await apiFetch(
		`/api/channels/${encodeURIComponent(channelId)}/threads/${encodeURIComponent(threadTs)}`,
		undefined,
		onUnauthorized
	)
	if (!res) return null
	if (res.status === 404) {
		return { parent: null, replies: [], notFound: true }
	}
	if (!res.ok) throw new ApiError(res.status)
	return (await res.json()) as ThreadResponse
}

/** チャンネルまたはスレッドにメッセージを送信する。 */
export async function postChatMessage(
	channelId: string,
	text: string,
	threadTs?: string,
	onUnauthorized?: UnauthorizedHandler
): Promise<Response | null> {
	return apiFetch(
		`/api/channels/${encodeURIComponent(channelId)}/messages`,
		{
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ text, thread_ts: threadTs || '' })
		},
		onUnauthorized
	)
}

/** メッセージをキーワード検索する。 */
export async function searchMessages(query: string, signal?: AbortSignal): Promise<SearchResult[]> {
	const res = await apiFetch(`/api/search?q=${encodeURIComponent(query)}`, { signal })
	if (!res) return []
	if (!res.ok) throw new ApiError(res.status)
	return (await res.json()) as SearchResult[]
}

/** リアルタイムイベントを受信する SSE 接続を作成する。 */
export function createChatEventSource(): EventSource {
	return new EventSource(`${API_BASE}/api/events`, { withCredentials: true })
}
