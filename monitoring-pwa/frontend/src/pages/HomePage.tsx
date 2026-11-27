import { Page } from '@nuage/ui'
import ChannelNav from '../components/ChannelNav'
import { useChatState } from '../state/chatStore'

/** スマホのホームタブ (チャンネルと DM の一覧) である。 */
export default function HomePage() {
	const bot = useChatState((s) => s.bot)
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
			<ChannelNav />
		</Page>
	)
}
