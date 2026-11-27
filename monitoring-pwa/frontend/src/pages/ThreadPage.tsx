import { useEffect, useMemo, useRef, useState } from 'react'
import { BackButton, Button, EmptyState, IconButton, LoadingState, Page, useIsTopPage, useIsWide } from '@nuage/ui'
import { X } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import Composer from '../components/Composer'
import MessageRow from '../components/MessageRow'
import MessageTimeline from '../components/MessageTimeline'
import NewMessagesButton from '../components/NewMessagesButton'
import NotificationDetailPanel from '../components/NotificationDetailPanel'
import { useChatScroll } from '../hooks/useChatScroll'
import { channelPath, useTabBase } from '../routes/paths'
import { channelTitle, threadKey, useChatActions, useChatState } from '../state/chatStore'
import { useChannelsById } from '../state/selectors'
import { messageToUnified } from '../utils/notification'
import { GRAFANA_BASE_URL } from '../utils/url'

const EMPTY: never[] = []

export default function ThreadPage() {
	const { channelId = '', ts = '' } = useParams<{ channelId: string; ts: string }>()
	const { loadThread, postMessage, enterChannel } = useChatActions()
	const user = useChatState((s) => s.user)
	const bot = useChatState((s) => s.bot)
	const channel = useChatState((s) => s.channels.find((c) => c.id === channelId))
	const data = useChatState((s) => s.threadData[threadKey(channelId, ts)])
	const channelsById = useChannelsById()
	const base = useTabBase()
	const isTop = useIsTopPage()
	const isWide = useIsWide()
	const navigate = useNavigate()
	const parent = data?.parent ?? null
	const replies = data?.replies ?? EMPTY
	const [sendError, setSendError] = useState<string | null>(null)
	const scrollRef = useRef<HTMLDivElement | null>(null)

	useEffect(() => {
		if (channelId && ts) void loadThread(channelId, ts)
	}, [channelId, ts, loadThread])

	useEffect(() => {
		if (!isTop || !channelId) return
		return enterChannel(channelId)
	}, [isTop, channelId, enterChannel])

	const itemIds = useMemo(() => (parent ? [parent.ts, ...replies.map((r) => r.ts)] : []), [parent, replies])
	const { handleScroll, hasNewMessages, scrollToBottom, stickToBottomOnNext } = useChatScroll({
		scrollRef,
		itemIds,
		hasMore: false,
		loadingOlder: false,
		onLoadOlder: () => {}
	})

	const unified = parent && (parent.details || parent.url || parent.source_kind) ? messageToUnified(parent) : null

	const handleSend = async (text: string) => {
		setSendError(null)
		stickToBottomOnNext()
		const res = await postMessage(channelId, text, ts)
		if (!res.ok) setSendError(res.error ?? '送信に失敗した。')
		return res.ok
	}

	// PC の横並びでは右の列に出すので、戻るの代わりに閉じる (スレッドを閉じて元の画面に戻る)
	const close = () => navigate(base === '/tabs/activity' ? base : channelPath(base, channelId))

	return (
		<Page
			className="chat-page"
			title="スレッド"
			subtitle={channelTitle(channel, bot)}
			start={isWide ? undefined : <BackButton />}
			end={isWide ? <IconButton label="スレッドを閉じる" icon={<X size={18} aria-hidden="true" />} onClick={close} /> : undefined}
			scrollRef={scrollRef}
			onScroll={handleScroll}
			footer={<Composer placeholder="スレッドに返信" onSend={handleSend} error={sendError} />}
		>
			<div className="chat-inner">
				{!data?.loaded && !parent ? (
					data?.error ? (
						<EmptyState>
							<p>{data.error}</p>
							<div className="empty-action">
								<Button onClick={() => void loadThread(channelId, ts)}>再読み込み</Button>
							</div>
						</EmptyState>
					) : (
						<LoadingState />
					)
				) : !parent ? (
					<EmptyState>このスレッドは見つからなかった。</EmptyState>
				) : (
					<>
						<MessageRow
							message={parent}
							currentUserId={user?.id ?? ''}
							currentUserName={user?.display_name ?? ''}
							botUserId={bot.user_id}
							botName={bot.name}
							channelsById={channelsById}
						/>
						{unified && (
							<div className="thread-detail">
								<NotificationDetailPanel item={unified} grafanaBase={GRAFANA_BASE_URL} />
							</div>
						)}
						<div className="replies-divider" role="separator">
							<span>{replies.length > 0 ? `${replies.length} 件の返信` : '返信はまだない'}</span>
						</div>
						<MessageTimeline
							messages={replies}
							currentUserId={user?.id ?? ''}
							currentUserName={user?.display_name ?? ''}
							botUserId={bot.user_id}
							botName={bot.name}
							channelsById={channelsById}
						/>
					</>
				)}
			</div>
			<NewMessagesButton visible={hasNewMessages} onClick={() => scrollToBottom(true)} />
		</Page>
	)
}
