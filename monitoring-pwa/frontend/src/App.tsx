import { useEffect } from 'react'
import { IonApp, IonRouterOutlet, IonSpinner, useIonRouter } from '@ionic/react'
import { IonReactRouter } from '@ionic/react-router'
import { Navigate, Route } from 'react-router-dom'
import { ChatProvider } from './state/ChatContext'
import { useChat } from './state/chatStore'
import { useVisualViewport } from './hooks/useVisualViewport'
import ChannelPage from './pages/ChannelPage'
import DeepLinkPage from './pages/DeepLinkPage'
import LoginPage from './pages/LoginPage'
import Tabs from './pages/Tabs'
import ThreadPage from './pages/ThreadPage'

// Service Worker から届く通知クリック (アプリが開いている時) を画面遷移に変える
function NotificationClickBridge() {
	const router = useIonRouter()
	useEffect(() => {
		if (!('serviceWorker' in navigator)) return
		const handle = (event: MessageEvent) => {
			if (event.data?.type === 'NOTIFICATION_CLICK' && typeof event.data.url === 'string') {
				router.push(event.data.url, 'forward', 'push')
			}
		}
		navigator.serviceWorker.addEventListener('message', handle)
		return () => navigator.serviceWorker.removeEventListener('message', handle)
	}, [router])
	return null
}

function Shell() {
	const { authState } = useChat()

	if (authState === 'loading') {
		return (
			<div className="boot-spinner">
				<IonSpinner name="crescent" />
			</div>
		)
	}
	if (authState === 'unauthenticated') {
		return <LoginPage />
	}
	return (
		<IonReactRouter>
			<NotificationClickBridge />
			<IonRouterOutlet>
				<Route path="/tabs/*" element={<Tabs />} />
				<Route path="/channels/:channelId" element={<ChannelPage />} />
				<Route path="/channels/:channelId/threads/:ts" element={<ThreadPage />} />
				<Route path="/alerts/:id" element={<DeepLinkPage kind="alert" />} />
				<Route path="/notifications/:id" element={<DeepLinkPage kind="notification" />} />
				<Route path="/settings" element={<Navigate to="/tabs/settings" replace />} />
				<Route path="/" element={<Navigate to="/tabs/home" replace />} />
				<Route path="*" element={<Navigate to="/tabs/home" replace />} />
			</IonRouterOutlet>
		</IonReactRouter>
	)
}

export default function App() {
	useVisualViewport()

	// iframe 内では描画しない (無限ネスト防止)
	if (window.self !== window.top) {
		return <div className="iframe-fallback">Nuage Notify is loaded inside an iframe.</div>
	}

	return (
		<IonApp>
			<ChatProvider>
				<Shell />
			</ChatProvider>
		</IonApp>
	)
}
