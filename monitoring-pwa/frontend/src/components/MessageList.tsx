import {
	useEffect,
	useLayoutEffect,
	useRef,
	useState,
	type KeyboardEvent,
	type UIEvent
} from 'react'
import { ArrowUp, AtSign, Hash, Bot, ChevronLeft, Search, Send, WifiOff } from 'lucide-react'
import MessageBubble from './MessageBubble'
import type { BotInfo, Channel, ChatMessage } from '../types/chat'

interface MessageListProps {
	channel: Channel
	messages: ChatMessage[]
	channelsById: Record<string, string>
	hasMore: boolean
	loadingOlder: boolean
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
	const scrollRef = useRef<HTMLDivElement | null>(null)
	const prevScrollHeightRef = useRef<number>(0)
	const prevFirstTsRef = useRef<string | null>(null)
	const prevLastTsRef = useRef<string | null>(null)
	const shouldStickBottomRef = useRef<boolean>(true)
	const loadingOlderRef = useRef<boolean>(false)

	useEffect(() => {
		loadingOlderRef.current = loadingOlder
	}, [loadingOlder])

	// チャンネル切り替え時は最下部へスクロール
	useEffect(() => {
		shouldStickBottomRef.current = true
		prevFirstTsRef.current = null
		prevLastTsRef.current = null
		setOfflineNotice(false)
	}, [channel.id])

	useLayoutEffect(() => {
		const el = scrollRef.current
		if (!el) return

		const firstTs = messages[0]?.ts || null
		const lastTs = messages[messages.length - 1]?.ts || null

		// 上方向へ古いメッセージが追加された場合はスクロール位置を維持する
		if (
			prevFirstTsRef.current &&
			firstTs &&
			firstTs < prevFirstTsRef.current &&
			prevScrollHeightRef.current > 0
		) {
			const diff = el.scrollHeight - prevScrollHeightRef.current
			el.scrollTop += diff
		} else if (shouldStickBottomRef.current || (lastTs && lastTs !== prevLastTsRef.current)) {
			el.scrollTop = el.scrollHeight
		}

		prevFirstTsRef.current = firstTs
		prevLastTsRef.current = lastTs
		prevScrollHeightRef.current = el.scrollHeight
	}, [messages, channel.id])

	const handleScroll = (e: UIEvent<HTMLDivElement>) => {
		const el = e.currentTarget
		const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
		shouldStickBottomRef.current = distanceFromBottom < 80

		if (el.scrollTop < 60 && hasMore && !loadingOlderRef.current) {
			prevScrollHeightRef.current = el.scrollHeight
			void onLoadOlder()
		}
	}

	const submitMessage = async () => {
		const text = draft.trim()
		if (!text || sending) return
		if (!bot.online) {
			setOfflineNotice(true)
		}
		const ok = await onSendMessage(text)
		if (ok) {
			setDraft('')
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

			<div ref={scrollRef} onScroll={handleScroll} className="chat-timeline">
				{hasMore && (
					<div className="load-older-wrap">
						<button
							type="button"
							disabled={loadingOlder}
							onClick={() => {
								if (scrollRef.current) {
									prevScrollHeightRef.current = scrollRef.current.scrollHeight
								}
								void onLoadOlder()
							}}
							className="load-older-btn"
						>
							<ArrowUp size={13} />
							{loadingOlder ? '読み込み中...' : 'さらに古い履歴を読み込む'}
						</button>
					</div>
				)}

				{messages.length === 0 ? (
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
