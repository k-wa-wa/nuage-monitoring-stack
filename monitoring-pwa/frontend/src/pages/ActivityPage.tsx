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
import { threadPath } from '../state/routes'
import type { ActivityItem, ActivityResponse, ChatMessage } from '../types/chat'

type ActivityFilter = 'all' | 'firing' | 'resolved' | 'notice'


function iconFor(m: ChatMessage) {
	if (m.source_kind === 'alert') {
		return m.alert_status === 'firing'
			? { icon: alertCircle, color: 'danger' }
			: { icon: checkmarkCircle, color: 'success' }
	}
	switch (m.level) {
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

// 本文 (Markdown) の 1 行目を見出し、残りを本文として、記号を落として出す
function summarize(text: string): { title: string; body: string } {
	const plain = (t: string) =>
		t
			.replace(/<[^>]+>/g, '')
			.replace(/[*_~`]/g, '')
			.replace(/^\s*[🔥✅]\s*/u, '')
			.replace(/^\[(firing|resolved)\]\s*/, '')
			.trim()
	const lines = text.split('\n').map(plain).filter(Boolean)
	return { title: lines[0] ?? '', body: lines.slice(1).join(' ') }
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
	const [items, setItems] = useState<ActivityItem[] | null>(null)
	const [firingCount, setFiringCount] = useState(0)
	const [filter, setFilter] = useState<ActivityFilter>('all')

	const fetchItems = useCallback(async () => {
		try {
			const res = await fetch(`${API_BASE}/api/activity?filter=${filter}&limit=100`, { credentials: 'include' })
			if (!res.ok) throw new Error(`HTTP ${res.status}`)
			const data = (await res.json()) as ActivityResponse
			setItems(data.items)
			setFiringCount(data.firing_count)
		} catch (err) {
			console.error('Failed to fetch activity:', err)
			setItems((prev) => prev ?? [])
		}
	}, [filter])

	useEffect(() => {
		void fetchItems()
	}, [fetchItems, activityVersion])

	const handleRefresh = async (e: CustomEvent<RefresherEventDetail>) => {
		await fetchItems()
		e.detail.complete()
	}

	const shown = useMemo(() => items ?? [], [items])

	return (
		<IonPage>
			<IonHeader translucent>
				<IonToolbar>
					<IonTitle>通知</IonTitle>
				</IonToolbar>
				<IonToolbar>
					<IonSegment
						value={filter}
						onIonChange={(e) => {
							setItems(null)
							setFilter((e.detail.value as ActivityFilter) ?? 'all')
						}}
						aria-label="通知の絞り込み"
					>
						<IonSegmentButton value="all">
							<IonLabel>すべて</IonLabel>
						</IonSegmentButton>
						<IonSegmentButton value="firing">
							<IonLabel>発生中{firingCount > 0 ? ` ${firingCount}` : ''}</IonLabel>
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
							{shown.map(({ channel_id, channel_name, message: m }) => {
								const { icon, color } = iconFor(m)
								const { title, body } = summarize(m.text)
								const resolvedIn = m.source_kind === 'alert' ? durationLabel(m.fired_at, m.resolved_at) : null
								return (
									<IonItem
										key={`${channel_id}-${m.ts}`}
										button
										detail={false}
										routerLink={threadPath('/tabs/activity', channel_id, m.ts)}
										className="activity-item"
									>
										<IonIcon slot="start" icon={icon} color={color} className="activity-icon" />
										<IonLabel>
											<div className="activity-head">
												<span className="activity-source"># {channel_name}</span>
												{m.source_kind === 'alert' && m.alert_status && (
													<span className={`msg-status ${m.alert_status === 'firing' ? 'firing' : 'resolved'}`}>
														{m.alert_status === 'firing' ? '発生中' : '解決済み'}
													</span>
												)}
											</div>
											<h2 className="activity-title">{title}</h2>
											{body && <p className="activity-body">{body}</p>}
											{resolvedIn && <p className="activity-sub">{resolvedIn}</p>}
										</IonLabel>
										<IonNote slot="end" className="activity-time">
											{formatMessageTime(m.fired_at || m.created_at)}
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
