import { useEffect, useState } from 'react'
import { Badge, EmptyState, ListItem, ListSection, SearchField, Spinner } from '@nuage/ui'
import { Bell, Bot, Hash, Settings } from 'lucide-react'
import { API_BASE, useChatState } from '../state/chatStore'
import { channelPath, threadPath } from '../state/routes'
import { formatMessageTime } from '../utils/date'
import type { SearchResult } from '../types/chat'

const SEARCH_DEBOUNCE_MS = 250

function stripMarkup(text: string): string {
	return text
		.replace(/<[^>]+>/g, '')
		.replace(/[*_~`>#]/g, '')
		.replace(/\s+/g, ' ')
		.trim()
}

interface ChannelNavProps {
	/** 今開いているパス (PC の横並びで選択中を示す) */
	activePath?: string
	/** 通知・設定への行も出すか (PC の横並びのサイドバー) */
	withAppLinks?: boolean
	firingCount?: number
}

/** 検索・チャンネル・DM の一覧である。スマホのホームタブと、PC のサイドバーで使う。 */
export default function ChannelNav({ activePath, withAppLinks = false, firingCount = 0 }: ChannelNavProps) {
	const channels = useChatState((s) => s.channels)
	const bot = useChatState((s) => s.bot)
	const [query, setQuery] = useState('')
	const [results, setResults] = useState<SearchResult[] | null>(null)
	const [searching, setSearching] = useState(false)

	useEffect(() => {
		const q = query.trim()
		if (!q) return
		const controller = new AbortController()
		const timer = window.setTimeout(async () => {
			setSearching(true)
			try {
				const res = await fetch(`${API_BASE}/api/search?q=${encodeURIComponent(q)}`, {
					credentials: 'include',
					signal: controller.signal
				})
				setResults(res.ok ? ((await res.json()) as SearchResult[]) : [])
			} catch (err) {
				if ((err as Error).name !== 'AbortError') setResults([])
			} finally {
				if (!controller.signal.aborted) setSearching(false)
			}
		}, SEARCH_DEBOUNCE_MS)
		return () => {
			controller.abort()
			window.clearTimeout(timer)
		}
	}, [query])

	const showResults = query.trim() !== '' && results !== null
	const isActive = (path: string) => activePath === path || Boolean(activePath?.startsWith(path + '/'))
	const publicChannels = channels.filter((c) => c.is_channel && !c.is_im)
	const dms = channels.filter((c) => c.is_im)

	return (
		<div className="channel-nav">
			<div className="channel-nav-search">
				<SearchField value={query} onChange={setQuery} placeholder="検索" />
			</div>

			{showResults && results ? (
				<ListSection title={searching ? <Spinner size={12} /> : '検索結果'}>
					{results.length === 0 && !searching ? (
						<ListItem title="見つからなかった" />
					) : (
						results.map((r) => (
							<ListItem
								key={`${r.channel_id}-${r.message.ts}`}
								className="search-result-item"
								to={threadPath('/tabs/home', r.channel_id, r.message.thread_ts || r.message.ts)}
								title={
									<>
										{r.is_im ? r.channel_name : `# ${r.channel_name}`}
										<span className="search-result-author"> · {r.message.username}</span>
									</>
								}
								subtitle={stripMarkup(r.message.text)}
								end={<span className="search-result-time">{formatMessageTime(r.message.created_at)}</span>}
							/>
						))
					)}
				</ListSection>
			) : (
				<>
					<ListSection title="チャンネル">
						{publicChannels.map((ch) => {
							const path = channelPath('/tabs/home', ch.id)
							return (
								<ListItem
									key={ch.id}
									className="channel-item"
									to={path}
									selected={isActive(path)}
									emphasized={ch.unread_count > 0}
									start={<Hash size={16} />}
									title={ch.name}
									end={<Badge count={ch.unread_count} />}
								/>
							)
						})}
						{publicChannels.length === 0 && <ListItem title="チャンネルはまだない" />}
					</ListSection>

					<ListSection title="ダイレクトメッセージ">
						{dms.map((ch) => {
							const path = channelPath('/tabs/home', ch.id)
							return (
								<ListItem
									key={ch.id}
									className="channel-item"
									to={path}
									selected={isActive(path)}
									emphasized={ch.unread_count > 0}
									start={
										<span className="dm-avatar">
											<Bot size={14} />
											<span className="dm-presence" data-online={bot.online ? 'true' : 'false'} />
										</span>
									}
									title={ch.name || bot.name}
									subtitle={bot.online ? 'オンライン' : 'オフライン'}
									end={<Badge count={ch.unread_count} />}
								/>
							)
						})}
						{dms.length === 0 && <ListItem title="DM はまだない" />}
					</ListSection>

					{withAppLinks && (
						<ListSection>
							<ListItem
								to="/tabs/activity"
								selected={isActive('/tabs/activity')}
								start={<Bell size={16} />}
								title="通知"
								end={<Badge count={firingCount} />}
							/>
							<ListItem to="/tabs/settings" selected={isActive('/tabs/settings')} start={<Settings size={16} />} title="設定" />
						</ListSection>
					)}
					{channels.length === 0 && <EmptyState>読み込み中…</EmptyState>}
				</>
			)}
		</div>
	)
}
