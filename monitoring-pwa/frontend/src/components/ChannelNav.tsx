import { Badge, ListItem, ListSection, SearchField } from '@nuage/ui'
import { Bell, Bot, Hash, Settings } from 'lucide-react'
import { formatMessageTime } from '../utils/date'
import type { BotInfo, Channel, SearchResult } from '../types/chat'

function stripMarkup(text: string): string {
	return text
		.replace(/<[^>]+>/g, '')
		.replace(/[*_~`>#]/g, '')
		.replace(/\s+/g, ' ')
		.trim()
}

interface ChannelNavProps {
	channels: Channel[]
	bot: BotInfo
	channelsLoading?: boolean
	query: string
	onQueryChange: (query: string) => void
	searchResults: SearchResult[] | null
	searching: boolean
	searchError?: string | null
	channelLinkFor: (channelId: string) => string
	threadLinkFor: (channelId: string, ts: string) => string
	/** 今開いているパス (PC の横並びで選択中を示す) */
	activePath?: string
	/** 通知・設定への行も出すか (PC の横並びのサイドバー) */
	withAppLinks?: boolean
	firingCount?: number
}

/** 検索・チャンネル・DM の一覧である。スマホのホームタブと、PC のサイドバーで使う純粋な表示部品。 */
export default function ChannelNav({
	channels,
	bot,
	channelsLoading = false,
	query,
	onQueryChange,
	searchResults,
	searching,
	searchError,
	channelLinkFor,
	threadLinkFor,
	activePath,
	withAppLinks = false,
	firingCount = 0
}: ChannelNavProps) {
	const hasQuery = query.trim() !== ''
	const isActive = (path: string) => activePath === path || Boolean(activePath?.startsWith(path + '/'))
	const publicChannels = channels.filter((c) => c.is_channel && !c.is_im)
	const dms = channels.filter((c) => c.is_im)

	return (
		<div className="channel-nav">
			<div className="channel-nav-search">
				<SearchField value={query} onChange={onQueryChange} placeholder="検索" />
			</div>

			{hasQuery ? (
				<ListSection title="検索結果" loading={searching}>
					{searchError ? (
						<ListItem title={searchError} />
					) : searching && (!searchResults || searchResults.length === 0) ? (
						<>
							<ListItem loading />
							<ListItem loading />
						</>
					) : searchResults && searchResults.length === 0 ? (
						<ListItem title="見つからなかった" />
					) : (
						(searchResults ?? []).map((r) => (
							<ListItem
								key={`${r.channel_id}-${r.message.ts}`}
								className="search-result-item"
								to={threadLinkFor(r.channel_id, r.message.thread_ts || r.message.ts)}
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
					<ListSection title="チャンネル" loading={channelsLoading}>
						{channelsLoading ? (
							<>
								<ListItem loading />
								<ListItem loading />
							</>
						) : publicChannels.length === 0 ? (
							<ListItem title="チャンネルはまだない" />
						) : (
							publicChannels.map((ch) => {
								const path = channelLinkFor(ch.id)
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
							})
						)}
					</ListSection>

					<ListSection title="ダイレクトメッセージ" loading={channelsLoading}>
						{channelsLoading ? (
							<ListItem loading />
						) : dms.length === 0 ? (
							<ListItem title="DM はまだない" />
						) : (
							dms.map((ch) => {
								const path = channelLinkFor(ch.id)
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
							})
						)}
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
				</>
			)}
		</div>
	)
}
