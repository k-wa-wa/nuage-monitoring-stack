import { AlertTriangle, Bell, Bot, CheckCircle, Info, MessageSquare, User, XCircle } from 'lucide-react'
import SlackRichText from './SlackRichText'
import { formatHistoryTimestamp } from '../utils/date'
import type { ChatMessage } from '../types/chat'

interface MessageBubbleProps {
	message: ChatMessage
	currentUserId: string
	botUserId: string
	botName: string
	channelsById?: Record<string, string>
	inThread?: boolean
	onOpenThread?: (msg: ChatMessage) => void
}

function renderMessageAvatar(msg: ChatMessage, isSelf: boolean) {
	if (msg.source_kind === 'alert') {
		return msg.alert_status === 'resolved' ? (
			<CheckCircle size={16} className="avatar-icon success" />
		) : (
			<AlertTriangle size={16} className="avatar-icon warn" />
		)
	}
	if (msg.bot_id) {
		switch (msg.level) {
			case 'error':
				return <XCircle size={16} className="avatar-icon warn" />
			case 'warning':
				return <AlertTriangle size={16} className="avatar-icon blocked" />
			case 'success':
				return <CheckCircle size={16} className="avatar-icon success" />
			default:
				return msg.source_kind === 'notification' ? (
					<Bell size={16} className="avatar-icon accent" />
				) : (
					<Bot size={16} className="avatar-icon accent" />
				)
		}
	}
	if (isSelf) {
		return <User size={16} className="avatar-icon self" />
	}
	return <Info size={16} className="avatar-icon" />
}

// 吹き出しスタイルのチャットメッセージである。
export default function MessageBubble({
	message,
	currentUserId,
	botUserId,
	botName,
	channelsById,
	inThread = false,
	onOpenThread
}: MessageBubbleProps) {
	const isSelf = !message.bot_id && message.user_id === currentUserId
	const isBot = Boolean(message.bot_id)
	const levelClass = message.level ? `level-${message.level}` : ''
	const hasDetail = Boolean(message.details || message.url)
	const replyCount = message.reply_count || 0
	const showThreadAction = !inThread && onOpenThread && (replyCount > 0 || hasDetail || !message.thread_ts)

	return (
		<article
			id={`chat-msg-${message.ts}`}
			data-msg-ts={message.ts}
			className={`bubble-row ${isSelf ? 'self' : 'other'}`}
		>
			{!isSelf && <div className={`bubble-avatar ${levelClass}`}>{renderMessageAvatar(message, false)}</div>}

			<div className={`bubble-card ${isSelf ? 'bubble-self' : 'bubble-other'} ${levelClass}`}>
				<div className="bubble-meta">
					<span className="bubble-author">{message.username || (isBot ? botName : 'User')}</span>
					{isBot && (
						<span className="bubble-badge-bot">
							{message.source_kind ? message.source_kind.toUpperCase() : 'BOT'}
						</span>
					)}
					{message.alert_status && (
						<span className={`status-badge ${message.alert_status === 'firing' ? 'badge-firing' : 'badge-resolved'}`}>
							{message.alert_status === 'firing' ? 'FIRING' : 'RESOLVED'}
						</span>
					)}
					<time className="bubble-time" dateTime={message.created_at} title={message.created_at}>
						{formatHistoryTimestamp(message.created_at)}
					</time>
				</div>

				<div className="bubble-body">
					<SlackRichText
						text={message.text}
						blocks={message.blocks}
						botUserId={botUserId}
						botName={botName}
						channelsById={channelsById}
					/>
				</div>

				{showThreadAction && (
					<div className="bubble-actions">
						<button
							type="button"
							onClick={() => onOpenThread?.(message)}
							className={`thread-open-btn ${replyCount > 0 ? 'has-replies' : ''}`}
						>
							<MessageSquare size={13} />
							{replyCount > 0
								? `${replyCount}件の返信${hasDetail ? '・詳細' : ''}`
								: hasDetail
								? '詳細・スレッド'
								: 'スレッドで返信'}
						</button>
					</div>
				)}
			</div>

			{isSelf && <div className="bubble-avatar self">{renderMessageAvatar(message, true)}</div>}
		</article>
	)
}
