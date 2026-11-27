import { Page } from '@nuage/ui'
import ChannelNav from '../components/ChannelNav'
import { useChannelSearch } from '../hooks/useChannelSearch'
import { channelPath, threadPath } from '../routes/paths'
import { useChatState } from '../state/chatStore'

/** スマホのホームタブ (チャンネルと DM の一覧) である。 */
export default function HomePage() {
	const bot = useChatState((s) => s.bot)
	const channels = useChatState((s) => s.channels)
	const bootstrapLoaded = useChatState((s) => s.bootstrapLoaded)
	const search = useChannelSearch()

	return (
		<Page
			title={<span className="app-title-tap">Nuage Notify</span>}
			subtitle={
				<span className="bot-status-line">
					<span className="status-dot" data-online={bot.online ? 'true' : 'false'} />
					{bot.name}: {bot.online ? 'オンライン' : 'オフライン'}
				</span>
			}
		>
			<ChannelNav
				channels={channels}
				bot={bot}
				channelsLoading={!bootstrapLoaded}
				query={search.query}
				onQueryChange={search.setQuery}
				searchResults={search.results}
				searching={search.searching}
				searchError={search.error}
				channelLinkFor={(id) => channelPath('/tabs/home', id)}
				threadLinkFor={(cid, ts) => threadPath('/tabs/home', cid, ts)}
			/>
		</Page>
	)
}
