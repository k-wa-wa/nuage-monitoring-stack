import { IonIcon } from '@ionic/react'
import {
	alertCircle,
	checkmarkCircle,
	chatbubbleOutline,
	closeCircle,
	hardwareChip,
	informationCircle,
	notifications,
	warning
} from 'ionicons/icons'
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

function avatarFor(msg: ChatMessage, isSelf: boolean, initial: string) {
	if (msg.source_kind === 'alert') {
		return msg.alert_status === 'resolved'
			? { icon: checkmarkCircle, tone: 'success' }
			: { icon: alertCircle, tone: 'danger' }
	}
	if (msg.bot_id) {
		switch (msg.level) {
			case 'error':
				return { icon: closeCircle, tone: 'danger' }
			case 'warning':
				return { icon: warning, tone: 'warning' }
			case 'success':
				return { icon: checkmarkCircle, tone: 'success' }
			default:
				return msg.source_kind === 'notification'
					? { icon: notifications, tone: 'primary' }
					: { icon: hardwareChip, tone: 'primary' }
		}
	}
	if (isSelf) return { initial, tone: 'self' }
	return { icon: informationCircle, tone: 'medium' }
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
						{'icon' in avatar && avatar.icon ? <IonIcon icon={avatar.icon} /> : avatar.initial}
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
				<div className="msg-body">
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
						<IonIcon icon={chatbubbleOutline} aria-hidden="true" />
						{replyCount > 0 ? `${replyCount} 件の返信` : '詳細を見る'}
					</Link>
				)}
			</div>
		</article>
	)
}
