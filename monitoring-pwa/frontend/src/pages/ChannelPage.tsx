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
import MessageTimeline from '../components/MessageTimeline'
import NewMessagesButton from '../components/NewMessagesButton'
import { useIonChatScroll } from '../hooks/useIonChatScroll'
import { channelTitle, useChat } from '../state/chatStore'
import { useChannelsById } from '../state/selectors'

export default function ChannelPage() {
	const { channelId = '' } = useParams<{ channelId: string }>()
	const { user, bot, channels, channelData, loadChannel, loadOlder, postMessage, enterChannel } =
		useChat()
	const channelsById = useChannelsById()
	const channel = channels.find((c) => c.id === channelId)
	const data = channelData[channelId]
	const messages = useMemo(() => data?.messages ?? [], [data])
	const [sendError, setSendError] = useState<string | null>(null)
	const contentRef = useRef<HTMLIonContentElement | null>(null)
	const leaveRef = useRef<(() => void) | null>(null)

	useEffect(() => {
		if (channelId) void loadChannel(channelId)
	}, [channelId, loadChannel])

	useIonViewDidEnter(() => {
		leaveRef.current?.()
		leaveRef.current = enterChannel(channelId)
	}, [channelId, enterChannel])
	useIonViewWillLeave(() => {
		leaveRef.current?.()
		leaveRef.current = null
	}, [])
	useEffect(() => () => leaveRef.current?.(), [])

	const itemIds = useMemo(() => messages.map((m) => m.ts), [messages])
	const { handleScroll, hasNewMessages, scrollToBottom, stickToBottomOnNext } = useIonChatScroll({
		contentRef,
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
		<IonPage className="chat-page">
			<IonHeader translucent>
				<IonToolbar>
					<IonButtons slot="start">
						<IonBackButton defaultHref={isDm ? '/tabs/dms' : '/tabs/home'} text="" />
					</IonButtons>
					<IonTitle>
						<div className="chat-title">
							<span className="chat-title-name">{title}</span>
							{isDm && (
								<span className={`chat-title-status ${bot.online ? 'online' : ''}`}>
									{bot.online ? 'オンライン' : 'オフライン'}
								</span>
							)}
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
				<div className="timeline-top">
					{data?.loadingOlder && <IonSpinner name="crescent" />}
					{data && !data.hasMore && data.loaded && channel && (
						<div className="channel-intro">
							<div className="channel-intro-title">{title}</div>
							<div className="channel-intro-text">
								{isDm
									? `${channel.name || bot.name} とのダイレクトメッセージの始まり`
									: `${title} チャンネルの始まり`}
							</div>
						</div>
					)}
				</div>
				{!data?.loaded ? (
					<div className="center-spinner">
						<IonSpinner name="crescent" />
					</div>
				) : (
					<MessageTimeline
						messages={messages}
						currentUserId={user?.id ?? ''}
						currentUserName={user?.display_name ?? ''}
						botUserId={bot.user_id}
						botName={bot.name}
						channelsById={channelsById}
						threadLinkFor={(m) =>
							`/channels/${encodeURIComponent(channelId)}/threads/${encodeURIComponent(m.ts)}`
						}
					/>
				)}
				</div>
				<NewMessagesButton visible={hasNewMessages} onClick={() => scrollToBottom(true)} />
			</IonContent>
			<IonFooter translucent>
				<Composer
					placeholder={placeholder}
					onSend={handleSend}
					error={sendError}
					notice={isDm && !bot.online ? `${bot.name} はオフライン。送ったメッセージは残るが、すぐには返事が来ない。` : null}
				/>
			</IonFooter>
		</IonPage>
	)
}
