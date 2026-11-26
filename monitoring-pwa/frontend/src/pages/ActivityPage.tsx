import { useCallback, useEffect, useMemo, useState } from 'react'
import {
	IonContent,
	IonHeader,
	IonIcon,
	IonItem,
	IonItemGroup,
	IonLabel,
	IonList,
	IonNote,
	IonPage,
	IonRefresher,
	IonRefresherContent,
	IonSegment,
	IonSegmentButton,
	IonSpinner,
	IonTitle,
	IonToolbar,
	type RefresherEventDetail
} from '@ionic/react'
import { alertCircle, checkmarkCircle, closeCircle, notifications, warning } from 'ionicons/icons'
import { API_BASE, useChat } from '../state/chatStore'
import { formatMessageTime } from '../utils/date'
import type { UnifiedNotification } from '../types/notification'
import { filterNotifications, type ActivityFilter } from '../utils/notification'


const SOURCE_LABEL: Record<string, string> = {
	alertmanager: 'Alertmanager',
	argocd: 'Argo CD',
	keel: 'Keel',
	generic: 'Webhook'
}

function iconFor(item: UnifiedNotification) {
	if (item.kind === 'alert') {
		return item.status === 'firing'
			? { icon: alertCircle, color: 'danger' }
			: { icon: checkmarkCircle, color: 'success' }
	}
	switch (item.level) {
		case 'error':
			return { icon: closeCircle, color: 'danger' }
		case 'warning':
			return { icon: warning, color: 'warning' }
		case 'success':
			return { icon: checkmarkCircle, color: 'success' }
		default:
			return { icon: notifications, color: 'primary' }
	}
}

function durationLabel(firedAt?: string, resolvedAt?: string): string | null {
	if (!firedAt || !resolvedAt) return null
	const ms = new Date(resolvedAt).getTime() - new Date(firedAt).getTime()
	if (!(ms > 0)) return null
	const min = Math.floor(ms / 60000)
	if (min < 1) return `${Math.floor(ms / 1000)}秒で解決`
	if (min < 60) return `${min}分で解決`
	return `${Math.floor(min / 60)}時間${min % 60}分で解決`
}


export default function ActivityPage() {
	const { activityVersion } = useChat()
	const [items, setItems] = useState<UnifiedNotification[] | null>(null)
	const [filter, setFilter] = useState<ActivityFilter>('all')

	const fetchItems = useCallback(async () => {
		try {
			const res = await fetch(`${API_BASE}/api/notifications`, { credentials: 'include' })
			if (res.ok) setItems((await res.json()) as UnifiedNotification[])
			else setItems((prev) => prev ?? [])
		} catch (err) {
			console.error('Failed to fetch notifications:', err)
			setItems((prev) => prev ?? [])
		}
	}, [])

	useEffect(() => {
		void fetchItems()
	}, [fetchItems, activityVersion])

	const handleRefresh = async (e: CustomEvent<RefresherEventDetail>) => {
		await fetchItems()
		e.detail.complete()
	}

	const all = useMemo(() => items ?? [], [items])
	const shown = useMemo(() => filterNotifications(all, filter), [all, filter])
	const count = (f: ActivityFilter) => filterNotifications(all, f).length

	return (
		<IonPage>
			<IonHeader translucent>
				<IonToolbar>
					<IonTitle>通知</IonTitle>
				</IonToolbar>
				<IonToolbar>
					<IonSegment
						value={filter}
						onIonChange={(e) => setFilter((e.detail.value as ActivityFilter) ?? 'all')}
						aria-label="通知の絞り込み"
					>
						<IonSegmentButton value="all">
							<IonLabel>すべて</IonLabel>
						</IonSegmentButton>
						<IonSegmentButton value="firing">
							<IonLabel>発生中{count('firing') > 0 ? ` ${count('firing')}` : ''}</IonLabel>
						</IonSegmentButton>
						<IonSegmentButton value="resolved">
							<IonLabel>解決済み</IonLabel>
						</IonSegmentButton>
						<IonSegmentButton value="notice">
							<IonLabel>一般</IonLabel>
						</IonSegmentButton>
					</IonSegment>
				</IonToolbar>
			</IonHeader>
			<IonContent fullscreen color="light">
				<IonRefresher slot="fixed" onIonRefresh={handleRefresh}>
					<IonRefresherContent />
				</IonRefresher>

				{items === null ? (
					<div className="center-spinner">
						<IonSpinner name="crescent" />
					</div>
				) : shown.length === 0 ? (
					<div className="empty-note">
						{filter === 'firing'
							? '発生中のアラートはない。'
							: filter === 'resolved'
								? '解決済みのアラートはない。'
								: filter === 'notice'
									? '一般の通知はない。'
									: '通知はまだない。'}
					</div>
				) : (
					<IonList inset className="activity-list">
						<IonItemGroup>
							{shown.map((item) => {
								const { icon, color } = iconFor(item)
								const link = item.kind === 'alert' ? `/alerts/${item.id}` : `/notifications/${item.id}`
								const resolvedIn = item.kind === 'alert' ? durationLabel(item.fired_at, item.resolved_at) : null
								return (
									<IonItem
										key={item.uid}
										button
										detail={false}
										routerLink={link}
										className="activity-item"
										data-uid={item.uid}
									>
										<IonIcon slot="start" icon={icon} color={color} className="activity-icon" />
										<IonLabel>
											<div className="activity-head">
												<span className="activity-source">{SOURCE_LABEL[item.source] ?? item.source}</span>
												{item.kind === 'alert' && (
													<span className={`msg-status ${item.status === 'firing' ? 'firing' : 'resolved'}`}>
														{item.status === 'firing' ? '発生中' : '解決済み'}
													</span>
												)}
											</div>
											<h2 className="activity-title">{item.title}</h2>
											<p className="activity-body">{item.body}</p>
											{resolvedIn && <p className="activity-sub">{resolvedIn}</p>}
										</IonLabel>
										<IonNote slot="end" className="activity-time">
											{formatMessageTime(item.fired_at || item.created_at)}
										</IonNote>
									</IonItem>
								)
							})}
						</IonItemGroup>
					</IonList>
				)}
			</IonContent>
		</IonPage>
	)
}
