import { createContext, useContext } from 'react'
import type { AuthUser, BotInfo, Channel, ChatMessage } from '../types/chat'

// ローカル開発時は別ポートで動くバックエンドを指す。
export const API_BASE = import.meta.env.DEV ? 'http://localhost:8080' : ''

export type AuthState = 'loading' | 'unauthenticated' | 'authenticated'

export interface ChannelData {
	messages: ChatMessage[]
	hasMore: boolean
	loaded: boolean
	loadingOlder: boolean
}

export interface ThreadData {
	parent: ChatMessage | null
	replies: ChatMessage[]
	loaded: boolean
}

export interface PostResult {
	ok: boolean
	error?: string
}

export interface ChatContextValue {
	authState: AuthState
	user: AuthUser | null
	bot: BotInfo
	channels: Channel[]
	channelData: Record<string, ChannelData>
	threadData: Record<string, ThreadData>
	// 通知 (アラート・Webhook) が増減するたびに増える。通知タブの再取得に使う。
	activityVersion: number
	reloadBootstrap: () => Promise<void>
	logout: () => Promise<void>
	loadChannel: (channelId: string) => Promise<void>
	loadOlder: (channelId: string) => Promise<void>
	loadThread: (channelId: string, threadTs: string) => Promise<void>
	postMessage: (channelId: string, text: string, threadTs?: string) => Promise<PostResult>
	// 画面に表示中のチャンネルを登録する。表示中は未読を数えず、既読にする。
	enterChannel: (channelId: string) => () => void
}

export const DEFAULT_BOT: BotInfo = { id: 'B01BOT', user_id: 'U01BOT', name: 'nuage-bot', online: false }

export const ChatContext = createContext<ChatContextValue | null>(null)

export const threadKey = (channelId: string, threadTs: string) => `${channelId}:${threadTs}`

export function useChat(): ChatContextValue {
	const ctx = useContext(ChatContext)
	if (!ctx) throw new Error('useChat は ChatProvider の内側で使う')
	return ctx
}

export function channelTitle(channel: Channel | undefined, bot: BotInfo): string {
	if (!channel) return ''
	if (channel.is_im) return channel.name || bot.name
	return `# ${channel.name}`
}
