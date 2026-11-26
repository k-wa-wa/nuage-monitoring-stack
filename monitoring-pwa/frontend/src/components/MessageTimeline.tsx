import { Fragment } from 'react'
import MessageRow from './MessageRow'
import { dayKey, formatDayLabel } from '../utils/date'
import type { ChatMessage } from '../types/chat'

interface MessageTimelineProps {
	messages: ChatMessage[]
	currentUserId: string
	currentUserName: string
	botUserId: string
	botName: string
	channelsById: Record<string, string>
	// メッセージからスレッド画面への URL を作る。渡さなければスレッドへのリンクを出さない。
	threadLinkFor?: (msg: ChatMessage) => string
}

const GROUP_WINDOW_MS = 5 * 60 * 1000

function authorKey(m: ChatMessage): string {
	return m.bot_id ? `bot:${m.bot_id}:${m.username}` : `user:${m.user_id ?? ''}`
}

// 日付の区切り線を入れ、同じ人が 5 分以内に続けた投稿はまとめて詰める (Slack と同じ)。
export default function MessageTimeline({
	messages,
	currentUserId,
	currentUserName,
	botUserId,
	botName,
	channelsById,
	threadLinkFor
}: MessageTimelineProps) {
	return (
		<div className="timeline">
			{messages.map((msg, i) => {
				const prev = i > 0 ? messages[i - 1] : null
				const newDay = !prev || dayKey(prev.created_at) !== dayKey(msg.created_at)
				const prevHasThread = prev ? Boolean(prev.reply_count || prev.details || prev.url) : false
				const compact =
					!newDay &&
					prev !== null &&
					!prevHasThread &&
					!msg.source_kind &&
					authorKey(prev) === authorKey(msg) &&
					new Date(msg.created_at).getTime() - new Date(prev.created_at).getTime() < GROUP_WINDOW_MS
				return (
					<Fragment key={msg.ts}>
						{newDay && (
							<div className="day-divider" role="separator">
								<span>{formatDayLabel(msg.created_at)}</span>
							</div>
						)}
						<MessageRow
							message={msg}
							currentUserId={currentUserId}
							currentUserName={currentUserName}
							botUserId={botUserId}
							botName={botName}
							channelsById={channelsById}
							threadLink={threadLinkFor?.(msg)}
							compact={compact}
						/>
					</Fragment>
				)
			})}
		</div>
	)
}
