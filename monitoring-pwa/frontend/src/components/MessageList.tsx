import {
	useEffect,
	useMemo,
	useState,
	type KeyboardEvent
} from 'react'
import { ArrowDown, ArrowUp, AtSign, Hash, Bot, ChevronLeft, Search, Send, WifiOff } from 'lucide-react'
import MessageBubble from './MessageBubble'
import { useChatScroll } from '../hooks/useChatScroll'
import type { BotInfo, Channel, ChatMessage } from '../types/chat'

interface MessageListProps {
	channel: Channel
	messages: ChatMessage[]
	channelsById: Record<string, string>
	hasMore: boolean
	loadingOlder: boolean
	loadingInitial?: boolean
	currentUserId: string
	bot: BotInfo
	sending: boolean
	sendError: string | null
	onBackMobile: () => void
	onLoadOlder: () => Promise<void>
	onSendMessage: (text: string) => Promise<boolean>
	onOpenThread: (msg: ChatMessage) => void
	onOpenSearch: () => void
}

// チャンネルまたは DM のメッセージタイムラインと入力欄である。
export default function MessageList({
	channel,
	messages,
	channelsById,
	hasMore,
	loadingOlder,
	loadingInitial = false,
	currentUserId,
	bot,
	sending,
	sendError,
	onBackMobile,
	onLoadOlder,
	onSendMessage,
	onOpenThread,
	onOpenSearch
}: MessageListProps) {
	const [draft, setDraft] = useState('')
	const [offlineNotice, setOfflineNotice] = useState(false)

	const itemIds = useMemo(() => messages.map((m) => m.ts), [messages])

	const {
		scrollRef,
		hasNewMessages,
		scrollToBottom,
		markStickBottomOnSend,
		handleScroll,
		prepareLoadOlder
	} = useChatScroll({
		scrollKey: channel.id,
		itemIds,
		isInitialLoading: loadingInitial,
		hasMore,
		loadingOlder,
		onLoadOlder
	})

	useEffect(() => {
		setOfflineNotice(false)
	}, [channel.id])

	const submitMessage = async () => {
		const text = draft.trim()
		if (!text || sending) return
		if (!bot.online) {
			setOfflineNotice(true)
		}
		markStickBottomOnSend()
		const ok = await onSendMessage(text)
		if (ok) {
			setDraft('')
			scrollToBottom()
		}
	}

	const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
		if (e.nativeEvent.isComposing) return
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault()
			void submitMessage()
		}
	}

	const insertMention = () => {
		const mention = `@${bot.name} `
		setDraft((prev) => (prev.startsWith(mention) ? prev : `${mention}${prev}`))
	}

	return (
		<section className="chat-main" aria-label={channel.is_im ? bot.name : `#${channel.name}`}>
			<header className="chat-header">
				<div className="chat-header-left">
					<button
						type="button"
						onClick={onBackMobile}
						className="icon-btn mobile-only-btn"
						aria-label="チャンネル一覧に戻る"
					>
						<ChevronLeft size={20} />
					</button>
					<div className="chat-header-title-wrap">
						<h2 className="chat-header-title">
							{channel.is_im ? <Bot size={17} /> : <Hash size={17} />}
							<span>{channel.is_im ? bot.name : channel.name}</span>
						</h2>
						{channel.topic && <span className="chat-header-topic">{channel.topic}</span>}
					</div>
				</div>

				<div className="chat-header-right">
					<span className={`connection-badge ${bot.online ? 'online' : ''}`}>
						<span className={`connection-dot ${bot.online ? 'online' : ''}`} />
						{bot.online ? 'Bot オンライン' : 'Bot オフライン'}
					</span>
					<button
						type="button"
						onClick={onOpenSearch}
						className="icon-btn mobile-only-btn"
						aria-label="検索"
					>
						<Search size={18} />
					</button>
				</div>
			</header>

			{!bot.online && (channel.is_im || offlineNotice) && (
				<div className="bot-offline-banner" role="status">
					<WifiOff size={15} />
					<span>
						{bot.name} は現在オフラインである（Socket Mode 未接続）。送信したメッセージは履歴に残るが、即時応答は返らない。
					</span>
				</div>
			)}

			<div className="chat-timeline-wrap">
				<div ref={scrollRef} onScroll={handleScroll} className="chat-timeline">
					{hasMore && (
						<div className="load-older-wrap">
							<button
								type="button"
								disabled={loadingOlder}
								onClick={() => {
									prepareLoadOlder()
									void onLoadOlder()
								}}
								className="load-older-btn"
							>
								<ArrowUp size={13} />
								{loadingOlder ? '読み込み中...' : 'さらに古い履歴を読み込む'}
							</button>
						</div>
					)}

					{messages.length === 0 && !loadingInitial ? (
						<div className="empty-state">
							{channel.is_im
								? `${bot.name} へのダイレクトメッセージはまだ存在しない。`
								: `#${channel.name} のメッセージはまだ存在しない。`}
						</div>
					) : (
						messages.map((msg) => (
							<MessageBubble
								key={msg.ts}
								message={msg}
								currentUserId={currentUserId}
								botUserId={bot.user_id}
								botName={bot.name}
								channelsById={channelsById}
								onOpenThread={onOpenThread}
							/>
						))
					)}
				</div>

				{hasNewMessages && (
					<button
						type="button"
						onClick={scrollToBottom}
						className="new-messages-btn"
						aria-label="最新のメッセージへ移動"
					>
						<ArrowDown size={14} />
						<span>新着あり ↓</span>
					</button>
				)}
			</div>

			<div className="composer-wrap">
				{sendError && (
					<div className="composer-error-banner" role="alert">
						{sendError}
					</div>
				)}
				<div className="composer-box">
					{channel.is_channel && (
						<div className="composer-toolbar">
							<button
								type="button"
								onClick={insertMention}
								className="mention-chip-btn"
								title="Bot にメンションを挿入"
							>
								<AtSign size={12} />
								{bot.name} にメンション
							</button>
							{!bot.online && <span className="composer-offline-hint">Bot オフライン</span>}
						</div>
					)}
					<div className="composer-input-row">
						<textarea
							rows={2}
							value={draft}
							onChange={(e) => setDraft(e.target.value)}
							onKeyDown={handleKeyDown}
							placeholder={
								channel.is_im
									? `${bot.name} へメッセージを送る...`
									: `#${channel.name} へメッセージを送る (@${bot.name} でメンション)...`
							}
							className="composer-textarea"
						/>
						<button
							type="button"
							onClick={() => void submitMessage()}
							disabled={sending || !draft.trim()}
							className="composer-send-btn"
							aria-label="送信"
						>
							<Send size={16} />
						</button>
					</div>
				</div>
			</div>
		</section>
	)
}
