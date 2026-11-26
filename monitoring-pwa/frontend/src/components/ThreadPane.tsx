import { useState, type KeyboardEvent } from 'react'
import { ChevronLeft, Send, WifiOff, X } from 'lucide-react'
import MessageBubble from './MessageBubble'
import NotificationDetailPanel from './NotificationDetailPanel'
import { GRAFANA_BASE_URL } from '../utils/url'
import type { BotInfo, ChatMessage } from '../types/chat'
import type { UnifiedNotification } from '../types/notification'

interface ThreadPaneProps {
	parent: ChatMessage
	replies: ChatMessage[]
	channelsById: Record<string, string>
	currentUserId: string
	bot: BotInfo
	sending: boolean
	sendError: string | null
	onClose: () => void
	onSendReply: (text: string) => Promise<boolean>
}

function messageToUnified(msg: ChatMessage): UnifiedNotification {
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

// 右ペイン（スマホでは全画面）に表示するスレッドおよび通知詳細パネルである。
export default function ThreadPane({
	parent,
	replies,
	channelsById,
	currentUserId,
	bot,
	sending,
	sendError,
	onClose,
	onSendReply
}: ThreadPaneProps) {
	const [draft, setDraft] = useState('')
	const hasDetailPanel = Boolean(parent.details || parent.url || parent.source_kind)
	const unifiedItem = hasDetailPanel ? messageToUnified(parent) : null

	const submitReply = async () => {
		const text = draft.trim()
		if (!text || sending) return
		const ok = await onSendReply(text)
		if (ok) {
			setDraft('')
		}
	}

	const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
		if (e.nativeEvent.isComposing) return
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault()
			void submitReply()
		}
	}

	return (
		<aside className="thread-pane" aria-label="スレッド">
			<header className="thread-header">
				<div className="thread-header-left">
					<button
						type="button"
						onClick={onClose}
						className="icon-btn mobile-only-btn"
						aria-label="スレッドを閉じる"
					>
						<ChevronLeft size={20} />
					</button>
					<h3 className="thread-title">スレッド</h3>
				</div>
				<button
					type="button"
					onClick={onClose}
					className="icon-btn desktop-only-btn"
					aria-label="スレッドを閉じる"
				>
					<X size={18} />
				</button>
			</header>

			<div className="thread-scroll">
				<div className="thread-parent">
					<MessageBubble
						message={parent}
						currentUserId={currentUserId}
						botUserId={bot.user_id}
						botName={bot.name}
						channelsById={channelsById}
						inThread
					/>
					{unifiedItem && (unifiedItem.details || unifiedItem.url) && (
						<div className="thread-detail-wrap">
							<NotificationDetailPanel item={unifiedItem} grafanaBase={GRAFANA_BASE_URL} />
						</div>
					)}
				</div>

				<div className="thread-divider">
					<span>{replies.length}件の返信</span>
				</div>

				<div className="thread-replies">
					{replies.length === 0 ? (
						<div className="thread-empty">まだ返信は存在しない。</div>
					) : (
						replies.map((reply) => (
							<MessageBubble
								key={reply.ts}
								message={reply}
								currentUserId={currentUserId}
								botUserId={bot.user_id}
								botName={bot.name}
								channelsById={channelsById}
								inThread
							/>
						))
					)}
				</div>
			</div>

			<div className="thread-composer">
				{sendError && (
					<div className="composer-error-banner" role="alert">
						{sendError}
					</div>
				)}
				{!bot.online && (
					<div className="thread-offline-hint">
						<WifiOff size={12} />
						<span>{bot.name} は現在オフラインである。</span>
					</div>
				)}
				<div className="composer-input-row">
					<textarea
						rows={2}
						value={draft}
						onChange={(e) => setDraft(e.target.value)}
						onKeyDown={handleKeyDown}
						placeholder="スレッドに返信..."
						className="composer-textarea"
					/>
					<button
						type="button"
						onClick={() => void submitReply()}
						disabled={sending || !draft.trim()}
						className="composer-send-btn"
						aria-label="スレッドに返信"
					>
						<Send size={16} />
					</button>
				</div>
			</div>
		</aside>
	)
}
