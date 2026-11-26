export interface AuthUser {
	id: string
	name: string
	display_name: string
}

export interface BotInfo {
	id: string
	user_id: string
	name: string
	online: boolean
}

export interface Channel {
	id: string
	name: string
	is_channel: boolean
	is_im: boolean
	dm_user_id?: string
	topic?: string
	purpose?: string
	created_at: string
	unread_count: number
	last_read_ts?: string
	latest_ts?: string
}

export interface SlackTextObject {
	type: 'mrkdwn' | 'plain_text' | string
	text: string
}

export interface SlackBlock {
	type: 'section' | 'context' | 'divider' | string
	text?: SlackTextObject
	fields?: SlackTextObject[]
	elements?: SlackTextObject[]
}

export interface ChatMessage {
	id: number
	channel_id: string
	ts: string
	thread_ts?: string
	user_id?: string
	bot_id?: string
	username: string
	text: string
	blocks?: SlackBlock[]
	level?: 'info' | 'success' | 'warning' | 'error' | string
	source_kind?: 'alert' | 'notification' | string
	details?: string
	url?: string
	alert_status?: 'firing' | 'resolved' | string
	fired_at?: string
	resolved_at?: string
	reply_count?: number
	latest_reply?: string
	created_at: string
	updated_at: string
}

export interface SearchResult {
	channel_id: string
	channel_name: string
	is_im: boolean
	message: ChatMessage
}

// 通知タブの 1 件 (アラート・通知のスレッドの親メッセージ)
export interface ActivityItem {
	channel_id: string
	channel_name: string
	message: ChatMessage
}

export interface ActivityResponse {
	items: ActivityItem[]
	has_more: boolean
	firing_count: number
}

export interface BootstrapResponse {
	user: AuthUser
	channels: Channel[]
	bot: BotInfo
}
