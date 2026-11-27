import type { Meta, StoryObj } from '@storybook/react-vite'
import { Bell, Home, Settings } from 'lucide-react'
import { MemoryRouter, useParams } from 'react-router-dom'
import { Page } from '../components/Page'
import { ListItem, ListSection } from '../components/List'
import { BackButton } from './BackButton'
import { SplitView } from './SplitView'
import { TabbedStacks } from './TabbedStacks'

const meta: Meta = { title: 'Navigation' }
export default meta

function HomeScreen() {
	return (
		<Page title="Nuage Notify" subtitle="nuage-bot: オンライン">
			<ListSection title="チャンネル">
				{['alertmanager', 'argocd', 'general'].map((name) => (
					<ListItem key={name} title={`# ${name}`} to={`/tabs/home/channels/${name}`} chevron />
				))}
			</ListSection>
		</Page>
	)
}

function ChannelScreen() {
	const { channelId } = useParams()
	return (
		<Page title={`# ${channelId}`} start={<BackButton />}>
			<div style={{ padding: 16 }}>
				{Array.from({ length: 40 }, (_, i) => (
					<p key={i}>メッセージ #{i + 1}</p>
				))}
			</div>
		</Page>
	)
}

function Simple({ title }: { title: string }) {
	return (
		<Page title={title}>
			<p style={{ padding: 16 }}>{title}</p>
		</Page>
	)
}

/** タブとタブごとの積み重ね。チャンネルを押すと押し込まれ、左上の戻るで戻る (タブバーは隠れる)。 */
export const Tabs: StoryObj = {
	render: () => (
		<div style={{ position: 'relative', height: 640, maxWidth: 390, border: '1px solid var(--line)', display: 'flex' }}>
			<MemoryRouter initialEntries={['/tabs/home']}>
				<TabbedStacks
					tabs={[
						{
							key: 'home',
							label: 'ホーム',
							icon: <Home size={20} />,
							badge: 2,
							basePath: '/tabs/home',
							routes: [
								{ path: '/tabs/home', element: <HomeScreen /> },
								{ path: '/tabs/home/channels/:channelId', element: <ChannelScreen /> }
							]
						},
						{
							key: 'activity',
							label: '通知',
							icon: <Bell size={20} />,
							basePath: '/tabs/activity',
							routes: [{ path: '/tabs/activity', element: <Simple title="通知" /> }]
						},
						{
							key: 'settings',
							label: '設定',
							icon: <Settings size={20} />,
							basePath: '/tabs/settings',
							routes: [{ path: '/tabs/settings', element: <Simple title="設定" /> }]
						}
					]}
				/>
			</MemoryRouter>
		</div>
	)
}

/** PC・タブレットの横並び。 */
export const Split: StoryObj = {
	render: () => (
		<div style={{ height: 560, display: 'flex', border: '1px solid var(--line)' }}>
			<MemoryRouter>
				<SplitView
					sidebar={
						<Page title="Nuage Notify">
							<ListSection title="チャンネル">
								<ListItem title="# general" selected to="#" />
								<ListItem title="# alertmanager" to="#" />
							</ListSection>
						</Page>
					}
					main={<Simple title="# general" />}
					aside={<Simple title="スレッド" />}
				/>
			</MemoryRouter>
		</div>
	)
}
