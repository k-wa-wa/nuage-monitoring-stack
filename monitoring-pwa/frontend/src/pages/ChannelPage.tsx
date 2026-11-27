import { useEffect, useMemo, useRef, useState } from 'react'
import { BackButton, Button, EmptyState, LoadingState, Page, Spinner, useIsTopPage } from '@nuage/ui'
import { useParams } from 'react-router-dom'
import Composer from '../components/Composer'
import MessageTimeline from '../components/MessageTimeline'
import NewMessagesButton from '../components/NewMessagesButton'
import { useChatScroll } from '../hooks/useChatScroll'
import { threadPath, useTabBase } from '../routes/paths'
import { channelTitle, useChatActions, useChatState } from '../state/chatStore'
import { useChannelsById } from '../state/selectors'

const EMPTY: never[] = []

export default function ChannelPage() {
	const { channelId = '' } = useParams<{ channelId: string }>()
	const { loadChannel, loadOlder, postMessage, enterChannel } = useChatActions()
	const user = useChatState((s) => s.user)
	const bot = useChatState((s) => s.bot)
	const channel = useChatState((s) => s.channels.find((c) => c.id === channelId))
	const data = useChatState((s) => s.channelData[channelId])
	const channelsById = useChannelsById()
	const base = useTabBase()
	const isTop = useIsTopPage()
	const messages = data?.messages ?? EMPTY
	const [sendError, setSendError] = useState<string | null>(null)
	const scrollRef = useRef<HTMLDivElement | null>(null)

	useEffect(() => {
		if (channelId) void loadChannel(channelId)
	}, [channelId, loadChannel])

	// いちばん手前に見えている間だけ「表示中」にする (未読を数えず、既読にする)
	useEffect(() => {
		if (!isTop || !channelId) return
		return enterChannel(channelId)
	}, [isTop, channelId, enterChannel])

	const itemIds = useMemo(() => messages.map((m) => m.ts), [messages])
	const { handleScroll, hasNewMessages, scrollToBottom, stickToBottomOnNext } = useChatScroll({
		scrollRef,
		itemIds,
		hasMore: Boolean(data?.hasMore),
		loadingOlder: Boolean(data?.loadingOlder),
		onLoadOlder: () => void loadOlder(channelId)
	})

	const title = channelTitle(channel, bot) || '読み込み中'
	const isDm = Boolean(channel?.is_im)
	const placeholder = isDm ? `${channel?.name || bot.name} へのメッセージ` : `${title} へのメッセージ`

	const handleSend = async (text: string) => {
		setSendError(null)
		stickToBottomOnNext()
		const res = await postMessage(channelId, text)
		if (!res.ok) setSendError(res.error ?? '送信に失敗した。')
		return res.ok
	}

	return (
		<Page
			className="chat-page"
			title={title}
			subtitle={isDm ? (bot.online ? 'オンライン' : 'オフライン') : channel?.topic}
			start={<BackButton />}
			scrollRef={scrollRef}
			onScroll={handleScroll}
			footer={
				<Composer
					placeholder={placeholder}
					onSend={handleSend}
					error={sendError}
					notice={isDm && !bot.online ? `${bot.name} はオフライン。送ったメッセージは残るが、すぐには返事が来ない。` : null}
				/>
			}
		>
			<div className="chat-inner">
				<div className="timeline-top">
					{data?.loadingOlder && <Spinner size={16} />}
					{data && !data.hasMore && data.loaded && channel && (
						<div className="channel-intro">
							<div className="channel-intro-title">{title}</div>
							<div className="channel-intro-text">
								{isDm ? `${channel.name || bot.name} とのダイレクトメッセージの始まり` : `${title} チャンネルの始まり`}
							</div>
						</div>
					)}
				</div>
				{!data?.loaded && messages.length === 0 ? (
					data?.error ? (
						<EmptyState>
							<p>{data.error}</p>
							<div className="empty-action">
								<Button onClick={() => void loadChannel(channelId)}>再読み込み</Button>
							</div>
						</EmptyState>
					) : (
						<LoadingState />
					)
				) : messages.length === 0 ? (
					<EmptyState>
						{isDm
							? `まだメッセージはない。${channel?.name || bot.name} に話しかけてみよう。`
							: 'まだメッセージはない。通知が届くとここに並ぶ。'}
					</EmptyState>
				) : (
					<MessageTimeline
						messages={messages}
						currentUserId={user?.id ?? ''}
						currentUserName={user?.display_name ?? ''}
						botUserId={bot.user_id}
						botName={bot.name}
						channelsById={channelsById}
						threadLinkFor={(m) => threadPath(base, channelId, m.ts)}
					/>
				)}
			</div>
			<NewMessagesButton visible={hasNewMessages} onClick={() => scrollToBottom(true)} />
		</Page>
	)
}
