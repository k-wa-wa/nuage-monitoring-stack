import { useEffect, useMemo, useState } from 'react'
import { AppShell, EmptyState, Page, Spinner, SplitView, TabbedStacks, ToastProvider, useIsWide, type TabDefinition } from '@nuage/ui'
import { Bell, Home, Settings } from 'lucide-react'
import { BrowserRouter, Navigate, useLocation, useNavigate, useParams, useRoutes } from 'react-router-dom'
import ChannelNav from './components/ChannelNav'
import ActivityPage from './pages/ActivityPage'
import ChannelPage from './pages/ChannelPage'
import HomePage from './pages/HomePage'
import LoginPage from './pages/LoginPage'
import SettingsPage from './pages/SettingsPage'
import ThreadPage from './pages/ThreadPage'
import { ChatProvider } from './state/ChatContext'
import { API_BASE, useChatState } from './state/chatStore'
import { parentPath } from './state/routes'
import type { ActivityResponse } from './types/chat'

// Service Worker から届く通知クリック (アプリが開いている時) を画面遷移に変える
function NotificationClickBridge() {
	const navigate = useNavigate()
	useEffect(() => {
		if (!('serviceWorker' in navigator)) return
		const handle = (event: MessageEvent) => {
			if (event.data?.type === 'NOTIFICATION_CLICK' && typeof event.data.url === 'string') {
				navigate(event.data.url)
			}
		}
		navigator.serviceWorker.addEventListener('message', handle)
		return () => navigator.serviceWorker.removeEventListener('message', handle)
	}, [navigate])
	return null
}

// 発生中のアラート数 (タブ・サイドバーのバッジ)
function useFiringCount(): number {
	const activityVersion = useChatState((s) => s.activityVersion)
	const [count, setCount] = useState(0)
	useEffect(() => {
		let cancelled = false
		fetch(`${API_BASE}/api/activity?filter=firing&limit=1`, { credentials: 'include' })
			.then((res) => (res.ok ? (res.json() as Promise<ActivityResponse>) : null))
			.then((data) => {
				if (!cancelled && data) setCount(data.firing_count)
			})
			.catch(() => {})
		return () => {
			cancelled = true
		}
	}, [activityVersion])
	return count
}

// Push 通知が開く URL (/channels/<ch>/threads/<ts>) を、タブの中のパスへ移す。
// バックエンドが画面のタブの作りを知らずに済むよう、Push には決まった形の URL を載せている
function ChannelLinkRedirect({ thread }: { thread: boolean }) {
	const { channelId = '', ts = '' } = useParams()
	const base = `/tabs/home/channels/${encodeURIComponent(channelId)}`
	return <Navigate to={thread ? `${base}/threads/${encodeURIComponent(ts)}` : base} replace />
}

/** スマホ: 下のタブと、タブごとの積み重ね (押し込み・左端スワイプで戻る) */
function MobileLayout({ firingCount }: { firingCount: number }) {
	const unread = useChatState((s) => s.channels.reduce((n, c) => n + c.unread_count, 0))
	const tabs = useMemo<TabDefinition[]>(
		() => [
			{
				key: 'home',
				label: 'ホーム',
				icon: <Home size={20} aria-hidden="true" />,
				badge: unread,
				basePath: '/tabs/home',
				parentPath,
				routes: [
					{ path: '/tabs/home', element: <HomePage /> },
					{ path: '/tabs/home/channels/:channelId', element: <ChannelPage /> },
					{ path: '/tabs/home/channels/:channelId/threads/:ts', element: <ThreadPage /> }
				]
			},
			{
				key: 'activity',
				label: '通知',
				icon: <Bell size={20} aria-hidden="true" />,
				badge: firingCount,
				basePath: '/tabs/activity',
				parentPath,
				routes: [
					{ path: '/tabs/activity', element: <ActivityPage /> },
					{ path: '/tabs/activity/channels/:channelId', element: <ChannelPage /> },
					{ path: '/tabs/activity/channels/:channelId/threads/:ts', element: <ThreadPage /> }
				]
			},
			{
				key: 'settings',
				label: '設定',
				icon: <Settings size={20} aria-hidden="true" />,
				basePath: '/tabs/settings',
				routes: [{ path: '/tabs/settings', element: <SettingsPage /> }]
			}
		],
		[unread, firingCount]
	)
	return <TabbedStacks tabs={tabs} />
}

/** PC・タブレット: 左にチャンネル一覧、真ん中に開いた画面、右にスレッド */
function WideLayout({ firingCount }: { firingCount: number }) {
	const { pathname } = useLocation()
	const bot = useChatState((s) => s.bot)
	const main = useRoutes([
		{ path: '/tabs/home', element: <EmptyState>左の一覧からチャンネルを選んでほしい。</EmptyState> },
		{ path: '/tabs/home/channels/:channelId', element: <ChannelPage /> },
		{ path: '/tabs/home/channels/:channelId/threads/:ts', element: <ChannelPage /> },
		{ path: '/tabs/activity/*', element: <ActivityPage /> },
		{ path: '/tabs/settings', element: <SettingsPage /> }
	])
	const aside = useRoutes([{ path: '/tabs/:tab/channels/:channelId/threads/:ts', element: <ThreadPage /> }])
	return (
		<SplitView
			sidebar={
				<Page
					title={<span className="app-title-tap">Nuage Notify</span>}
					subtitle={`${bot.name}: ${bot.online ? 'オンライン' : 'オフライン'}`}
				>
					<ChannelNav activePath={pathname} withAppLinks firingCount={firingCount} />
				</Page>
			}
			main={main}
			aside={aside}
		/>
	)
}

function AppLayout() {
	const isWide = useIsWide()
	const firingCount = useFiringCount()
	const { pathname } = useLocation()
	const redirects = useRoutes([
		{ path: '/channels/:channelId', element: <ChannelLinkRedirect thread={false} /> },
		{ path: '/channels/:channelId/threads/:ts', element: <ChannelLinkRedirect thread /> },
		{ path: '/tabs/*', element: null },
		{ path: '*', element: <Navigate to="/tabs/home" replace /> }
	])
	const known = ['/tabs/home', '/tabs/activity', '/tabs/settings'].some((p) => pathname === p || pathname.startsWith(p + '/'))
	if (!known) return redirects
	return isWide ? <WideLayout firingCount={firingCount} /> : <MobileLayout firingCount={firingCount} />
}

function Shell() {
	const authState = useChatState((s) => s.authState)
	if (authState === 'loading') {
		return (
			<div className="boot-spinner">
				<Spinner size={28} />
			</div>
		)
	}
	if (authState === 'unauthenticated') return <LoginPage />
	return (
		<>
			<NotificationClickBridge />
			<AppLayout />
		</>
	)
}

export default function App() {
	// iframe 内では描画しない (無限ネスト防止)
	if (window.self !== window.top) {
		return <div className="iframe-fallback">Nuage Notify is loaded inside an iframe.</div>
	}
	return (
		<BrowserRouter>
			<AppShell>
				<ToastProvider>
					<ChatProvider>
						<Shell />
					</ChatProvider>
				</ToastProvider>
			</AppShell>
		</BrowserRouter>
	)
}
