import { useEffect, useRef, useState } from 'react'
import {
	IonBackButton,
	IonButtons,
	IonContent,
	IonHeader,
	IonPage,
	IonSpinner,
	IonTitle,
	IonToolbar,
	useIonRouter
} from '@ionic/react'
import { useParams } from 'react-router-dom'
import { API_BASE } from '../state/chatStore'
import { threadPath } from '../state/routes'

// /alerts/:id と /notifications/:id (Push 通知・通知タブからの遷移先) を、
// その通知が投稿されたスレッドへ置き換える。
export default function DeepLinkPage({ kind }: { kind: 'alert' | 'notification' }) {
	const { id = '' } = useParams<{ id: string }>()
	const router = useIonRouter()
	const routerRef = useRef(router)
	routerRef.current = router
	const [failed, setFailed] = useState(false)

	useEffect(() => {
		let cancelled = false
		setFailed(false)
		fetch(`${API_BASE}/api/deeplink/${kind}/${encodeURIComponent(id)}`, { credentials: 'include' })
			.then((res) => (res.ok ? res.json() : null))
			.then((data: { channel_id?: string; ts?: string } | null) => {
				if (cancelled) return
				if (data?.channel_id && data.ts) {
					routerRef.current.push(
						threadPath('/tabs/activity', data.channel_id, data.ts),
						'none',
						'replace'
					)
				} else {
					setFailed(true)
				}
			})
			.catch(() => {
				if (!cancelled) setFailed(true)
			})
		return () => {
			cancelled = true
		}
	}, [kind, id])

	return (
		<IonPage>
			<IonHeader translucent>
				<IonToolbar>
					<IonButtons slot="start">
						<IonBackButton defaultHref="/tabs/activity" text="" />
					</IonButtons>
					<IonTitle>{kind === 'alert' ? 'アラート' : '通知'}</IonTitle>
				</IonToolbar>
			</IonHeader>
			<IonContent fullscreen>
				{failed ? (
					<div className="empty-note">この通知は見つからなかった。</div>
				) : (
					<div className="center-spinner">
						<IonSpinner name="crescent" />
					</div>
				)}
			</IonContent>
		</IonPage>
	)
}
