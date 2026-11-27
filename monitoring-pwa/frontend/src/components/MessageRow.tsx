import type { LucideIcon } from 'lucide-react'
import { AlertTriangle, Bell, Bot, CheckCircle2, CircleAlert, Info, MessageSquare, XCircle } from 'lucide-react'
import { Link } from 'react-router-dom'
import SlackRichText from './SlackRichText'
import { formatMessageTime } from '../utils/date'
import type { ChatMessage } from '../types/chat'

interface MessageRowProps {
	message: ChatMessage
	currentUserId: string
	currentUserName: string
	botUserId: string
	botName: string
	channelsById?: Record<string, string>
	// スレッドへのリンクを出すか (チャンネル画面のみ)
	threadLink?: string
	// 直前と同じ投稿者が続く場合は、アイコンと名前を省いて詰める (Slack と同じ)
	compact?: boolean
}

type Avatar = { icon?: LucideIcon; initial?: string; tone: 'accent' | 'success' | 'warn' | 'blocked' | 'muted' | 'self' }

function avatarFor(msg: ChatMessage, isSelf: boolean, initial: string): Avatar {
	if (msg.source_kind === 'alert') {
		return msg.alert_status === 'resolved' ? { icon: CheckCircle2, tone: 'success' } : { icon: CircleAlert, tone: 'warn' }
	}
	if (msg.bot_id) {
		switch (msg.level) {
			case 'error':
				return { icon: XCircle, tone: 'warn' }
			case 'warning':
				return { icon: AlertTriangle, tone: 'blocked' }
			case 'success':
				return { icon: CheckCircle2, tone: 'success' }
			default:
				return msg.source_kind === 'notification' ? { icon: Bell, tone: 'accent' } : { icon: Bot, tone: 'accent' }
		}
	}
	if (isSelf) return { initial, tone: 'self' }
	return { icon: Info, tone: 'muted' }
}

// Slack と同じく、アイコン・名前・時刻・本文を左寄せで並べるメッセージ行である。
export default function MessageRow({
	message,
	currentUserId,
	currentUserName,
	botUserId,
	botName,
	channelsById,
	threadLink,
	compact = false
}: MessageRowProps) {
	const isSelf = !message.bot_id && message.user_id === currentUserId
	const author = message.username || (message.bot_id ? botName : currentUserName || 'User')
	const avatar = avatarFor(message, isSelf, (author || '?').slice(0, 1).toUpperCase())
	const replyCount = message.reply_count || 0
	const hasDetail = Boolean(message.details || message.url)

	return (
		<article
			id={`chat-msg-${message.ts}`}
			data-msg-ts={message.ts}
			className={`msg-row ${compact ? 'compact' : ''} ${message.level ? `level-${message.level}` : ''}`}
		>
			<div className="msg-gutter">
				{compact ? (
					<time className="msg-gutter-time" dateTime={message.created_at}>
						{formatMessageTime(message.created_at, true)}
					</time>
				) : (
					<div className={`msg-avatar tone-${avatar.tone}`} aria-hidden="true">
						{avatar.icon ? <avatar.icon size={18} /> : avatar.initial}
					</div>
				)}
			</div>
			<div className="msg-main">
				{!compact && (
					<div className="msg-meta">
						<span className="msg-author">{author}</span>
						{message.bot_id && <span className="msg-app-badge">APP</span>}
						<time className="msg-time" dateTime={message.created_at} title={message.created_at}>
							{formatMessageTime(message.created_at)}
						</time>
					</div>
				)}
				{message.alert_status && (
					<span className={`msg-status ${message.alert_status === 'firing' ? 'firing' : 'resolved'}`}>
						{message.alert_status === 'firing' ? '発生中' : '解決済み'}
					</span>
				)}
				<div className="msg-body nui-selectable">
					<SlackRichText
						text={message.text}
						blocks={message.blocks}
						botUserId={botUserId}
						botName={botName}
						channelsById={channelsById}
					/>
				</div>
				{threadLink && (replyCount > 0 || hasDetail) && (
					<Link to={threadLink} className="msg-thread-link">
						<MessageSquare size={13} aria-hidden="true" />
						{replyCount > 0 ? `${replyCount} 件の返信` : '詳細を見る'}
					</Link>
				)}
			</div>
		</article>
	)
}
