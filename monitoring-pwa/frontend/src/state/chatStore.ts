import { createContext, useContext, useSyncExternalStore } from 'react'
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

export interface ChatState {
	authState: AuthState
	user: AuthUser | null
	bot: BotInfo
	channels: Channel[]
	channelData: Record<string, ChannelData>
	threadData: Record<string, ThreadData>
	// 通知 (アラート・Webhook) が増減するたびに増える。通知タブの再取得に使う。
	activityVersion: number
}

export interface ChatActions {
	reloadBootstrap: () => Promise<void>
	logout: () => Promise<void>
	loadChannel: (channelId: string) => Promise<void>
	loadOlder: (channelId: string) => Promise<void>
	loadThread: (channelId: string, threadTs: string) => Promise<void>
	postMessage: (channelId: string, text: string, threadTs?: string) => Promise<PostResult>
	// 画面に表示中のチャンネルを登録する。表示中は未読を数えず、既読にする。
	enterChannel: (channelId: string) => () => void
}

type Updater<T> = T | ((prev: T) => T)

/**
 * チャットの状態を持つ小さなストアである。画面は useChatState(選び方) で要る部分だけを購読し、
 * 関係の無い更新 (別のチャンネルへの新着など) では描き直されない。
 */
export class ChatStore {
	private state: ChatState
	private listeners = new Set<() => void>()

	constructor(initial: ChatState) {
		this.state = initial
	}

	get = (): ChatState => this.state

	subscribe = (listener: () => void): (() => void) => {
		this.listeners.add(listener)
		return () => this.listeners.delete(listener)
	}

	/** 1 つの項目を更新する (React の setState と同じく、値か「前の値を受け取る関数」を渡す)。 */
	setter<K extends keyof ChatState>(key: K): (next: Updater<ChatState[K]>) => void {
		return (next) => {
			const prev = this.state[key]
			const value = typeof next === 'function' ? (next as (p: ChatState[K]) => ChatState[K])(prev) : next
			if (Object.is(value, prev)) return
			this.state = { ...this.state, [key]: value }
			this.listeners.forEach((l) => l())
		}
	}
}

export interface ChatContextValue {
	store: ChatStore
	actions: ChatActions
}

export const DEFAULT_BOT: BotInfo = { id: 'B01BOT', user_id: 'U01BOT', name: 'nuage-bot', online: false }

export const ChatContext = createContext<ChatContextValue | null>(null)

export const threadKey = (channelId: string, threadTs: string) => `${channelId}:${threadTs}`

function useChatContext(): ChatContextValue {
	const ctx = useContext(ChatContext)
	if (!ctx) throw new Error('useChatState / useChatActions は ChatProvider の内側で使う')
	return ctx
}

/** チャットの状態のうち、selector で選んだ部分だけを購読する。選んだ値が変わった時だけ描き直す。 */
export function useChatState<T>(selector: (s: ChatState) => T): T {
	const { store } = useChatContext()
	return useSyncExternalStore(store.subscribe, () => selector(store.get()))
}

/** チャットの操作 (読み込み・投稿・既読など)。参照は変わらない。 */
export function useChatActions(): ChatActions {
	return useChatContext().actions
}

export function channelTitle(channel: Channel | undefined, bot: BotInfo): string {
	if (!channel) return ''
	if (channel.is_im) return channel.name || bot.name
	return `# ${channel.name}`
}
