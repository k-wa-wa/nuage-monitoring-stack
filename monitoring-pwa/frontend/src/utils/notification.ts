import type { ChatMessage } from '../types/chat'
import type { UnifiedNotification } from '../types/notification'

// messageToUnified はチャットのメッセージ (通知の投稿) を、通知の詳細表示用の形に直す。
export function messageToUnified(msg: ChatMessage): UnifiedNotification {
	const kind = msg.source_kind === 'alert' ? 'alert' : 'notification'
	const status =
		kind === 'alert'
			? msg.alert_status === 'resolved'
				? 'resolved'
				: 'firing'
			: 'notice'
	const level =
		msg.level === 'error' || msg.level === 'warning' || msg.level === 'success' || msg.level === 'info'
			? msg.level
			: 'info'

	return {
		uid: `${kind}-${msg.source_id || msg.id}`,
		id: msg.source_id || msg.id,
		kind,
		source: msg.username.toLowerCase(),
		title: msg.username,
		body: msg.text,
		status,
		level,
		url: msg.url,
		details: msg.details,
		timestamp: msg.updated_at || msg.created_at,
		fired_at: msg.fired_at,
		resolved_at: msg.resolved_at,
		created_at: msg.created_at,
		updated_at: msg.updated_at
	}
}

export type ActivityFilter = 'all' | 'firing' | 'resolved' | 'notice'

// filterNotifications は通知タブの絞り込み (すべて・発生中・解決済み・一般) を行う。
export function filterNotifications(items: UnifiedNotification[], filter: ActivityFilter) {
	switch (filter) {
		case 'firing':
			return items.filter((n) => n.kind === 'alert' && n.status === 'firing')
		case 'resolved':
			return items.filter((n) => n.kind === 'alert' && n.status === 'resolved')
		case 'notice':
			return items.filter((n) => n.kind === 'notification')
		default:
			return items
	}
}
