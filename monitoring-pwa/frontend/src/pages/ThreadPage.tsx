import { useEffect, useMemo, useRef, useState } from 'react'
import {
	IonBackButton,
	IonButtons,
	IonContent,
	IonFooter,
	IonHeader,
	IonPage,
	IonSpinner,
	IonTitle,
	IonToolbar,
	useIonViewDidEnter,
	useIonViewWillLeave
} from '@ionic/react'
import { useParams } from 'react-router-dom'
import Composer from '../components/Composer'
import MessageRow from '../components/MessageRow'
import MessageTimeline from '../components/MessageTimeline'
import NewMessagesButton from '../components/NewMessagesButton'
import NotificationDetailPanel from '../components/NotificationDetailPanel'
import { useIonChatScroll } from '../hooks/useIonChatScroll'
import { channelTitle, threadKey, useChat } from '../state/chatStore'
import { useChannelsById } from '../state/selectors'
import { GRAFANA_BASE_URL } from '../utils/url'
import { messageToUnified } from '../utils/notification'

export default function ThreadPage() {
	const { channelId = '', ts = '' } = useParams<{ channelId: string; ts: string }>()
	const { user, bot, channels, threadData, loadThread, postMessage, enterChannel } = useChat()
	const channelsById = useChannelsById()
	const channel = channels.find((c) => c.id === channelId)
	const data = threadData[threadKey(channelId, ts)]
	const parent = data?.parent ?? null
	const replies = useMemo(() => data?.replies ?? [], [data])
	const [sendError, setSendError] = useState<string | null>(null)
	const contentRef = useRef<HTMLIonContentElement | null>(null)
	const leaveRef = useRef<(() => void) | null>(null)

	useEffect(() => {
		if (channelId && ts) void loadThread(channelId, ts)
	}, [channelId, ts, loadThread])

	useIonViewDidEnter(() => {
		leaveRef.current?.()
		leaveRef.current = enterChannel(channelId)
	}, [channelId, enterChannel])
	useIonViewWillLeave(() => {
		leaveRef.current?.()
		leaveRef.current = null
	}, [])
	useEffect(() => () => leaveRef.current?.(), [])

	const itemIds = useMemo(
		() => (parent ? [parent.ts, ...replies.map((r) => r.ts)] : []),
		[parent, replies]
	)
	const { handleScroll, hasNewMessages, scrollToBottom, stickToBottomOnNext } = useIonChatScroll({
		contentRef,
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

	return (
		<IonPage className="chat-page">
			<IonHeader translucent>
				<IonToolbar>
					<IonButtons slot="start">
						<IonBackButton defaultHref={`/channels/${encodeURIComponent(channelId)}`} text="" />
					</IonButtons>
					<IonTitle>
						<div className="chat-title">
							<span className="chat-title-name">スレッド</span>
							<span className="chat-title-status">{channelTitle(channel, bot)}</span>
						</div>
					</IonTitle>
				</IonToolbar>
			</IonHeader>
			<IonContent
				ref={contentRef}
				fullscreen
				scrollEvents
				onIonScroll={handleScroll}
				className="chat-content"
			>
				<div className="chat-inner">
				{!data?.loaded && !parent ? (
					<div className="center-spinner">
						<IonSpinner name="crescent" />
					</div>
				) : !parent ? (
					<div className="empty-note">このスレッドは見つからなかった。</div>
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
			</IonContent>
			<IonFooter translucent>
				<Composer placeholder="スレッドに返信" onSend={handleSend} error={sendError} />
			</IonFooter>
		</IonPage>
	)
}
