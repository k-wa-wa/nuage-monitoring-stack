import { useEffect, useState } from 'react'
import { IonBadge, IonIcon, IonLabel, IonRouterOutlet, IonTabBar, IonTabButton, IonTabs } from '@ionic/react'
import { Navigate, Route } from 'react-router-dom'
import { home, notifications, settings } from 'ionicons/icons'
import ActivityPage from './ActivityPage'
import ChannelPage from './ChannelPage'
import DeepLinkPage from './DeepLinkPage'
import ThreadPage from './ThreadPage'
import HomePage from './HomePage'
import SettingsPage from './SettingsPage'
import { API_BASE, useChat } from '../state/chatStore'
import type { UnifiedNotification } from '../types/notification'

// 発生中のアラート数を通知タブのバッジに出す
function useFiringCount(): number {
	const { activityVersion } = useChat()
	const [count, setCount] = useState(0)
	useEffect(() => {
		let cancelled = false
		fetch(`${API_BASE}/api/notifications`, { credentials: 'include' })
			.then((res) => (res.ok ? res.json() : []))
			.then((items: UnifiedNotification[]) => {
				if (!cancelled) {
					setCount(items.filter((n) => n.kind === 'alert' && n.status === 'firing').length)
				}
			})
			.catch(() => {})
		return () => {
			cancelled = true
		}
	}, [activityVersion])
	return count
}

export default function Tabs() {
	const { channels } = useChat()
	const firing = useFiringCount()
	// ホームにチャンネルと DM を並べるので、両方の未読を合わせて出す
	const unread = channels.reduce((n, c) => n + c.unread_count, 0)

	return (
		<IonTabs>
			<IonRouterOutlet>
				<Route path="home" element={<HomePage />} />
				<Route path="home/channels/:channelId" element={<ChannelPage />} />
				<Route path="home/channels/:channelId/threads/:ts" element={<ThreadPage />} />
				<Route path="dms" element={<Navigate to="/tabs/home" replace />} />
				<Route path="activity" element={<ActivityPage />} />
				<Route path="activity/alerts/:id" element={<DeepLinkPage kind="alert" />} />
				<Route path="activity/notifications/:id" element={<DeepLinkPage kind="notification" />} />
				<Route path="activity/channels/:channelId" element={<ChannelPage />} />
				<Route path="activity/channels/:channelId/threads/:ts" element={<ThreadPage />} />
				<Route path="settings" element={<SettingsPage />} />
				<Route index element={<Navigate to="home" replace />} />
			</IonRouterOutlet>
			<IonTabBar slot="bottom">
				<IonTabButton tab="home" href="/tabs/home">
					<IonIcon icon={home} aria-hidden="true" />
					<IonLabel>ホーム</IonLabel>
					{unread > 0 && <IonBadge color="danger">{unread}</IonBadge>}
				</IonTabButton>
				<IonTabButton tab="activity" href="/tabs/activity">
					<IonIcon icon={notifications} aria-hidden="true" />
					<IonLabel>通知</IonLabel>
					{firing > 0 && <IonBadge color="danger">{firing}</IonBadge>}
				</IonTabButton>
				<IonTabButton tab="settings" href="/tabs/settings">
					<IonIcon icon={settings} aria-hidden="true" />
					<IonLabel>設定</IonLabel>
				</IonTabButton>
			</IonTabBar>
		</IonTabs>
	)
}
