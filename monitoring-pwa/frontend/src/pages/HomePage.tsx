import { useEffect, useState } from 'react'
import {
	IonBadge,
	IonContent,
	IonHeader,
	IonIcon,
	IonItem,
	IonItemGroup,
	IonLabel,
	IonList,
	IonListHeader,
	IonNote,
	IonPage,
	IonSearchbar,
	IonSpinner,
	IonTitle,
	IonToolbar
} from '@ionic/react'
import { hardwareChip } from 'ionicons/icons'
import { API_BASE, useChat } from '../state/chatStore'
import { formatMessageTime } from '../utils/date'
import type { SearchResult } from '../types/chat'

const SEARCH_DEBOUNCE_MS = 250

function stripMarkup(text: string): string {
	return text
		.replace(/<[^>]+>/g, '')
		.replace(/[*_~`>#]/g, '')
		.replace(/\s+/g, ' ')
		.trim()
}

export default function HomePage() {
	const { channels, bot } = useChat()
	const [query, setQuery] = useState('')
	const [results, setResults] = useState<SearchResult[] | null>(null)
	const [searching, setSearching] = useState(false)

	useEffect(() => {
		const q = query.trim()
		if (!q) {
			setResults(null)
			setSearching(false)
			return
		}
		setSearching(true)
		const controller = new AbortController()
		const timer = window.setTimeout(async () => {
			try {
				const res = await fetch(`${API_BASE}/api/search?q=${encodeURIComponent(q)}`, {
					credentials: 'include',
					signal: controller.signal
				})
				setResults(res.ok ? ((await res.json()) as SearchResult[]) : [])
			} catch (err) {
				if ((err as Error).name !== 'AbortError') setResults([])
			} finally {
				if (!controller.signal.aborted) setSearching(false)
			}
		}, SEARCH_DEBOUNCE_MS)
		return () => {
			controller.abort()
			window.clearTimeout(timer)
		}
	}, [query])

	const publicChannels = channels.filter((c) => c.is_channel && !c.is_im)
	const dms = channels.filter((c) => c.is_im)

	return (
		<IonPage>
			<IonHeader translucent>
				<IonToolbar>
					<IonTitle>
						<div className="home-title app-title-tap">
							<span className="home-title-name">Nuage Notify</span>
							<span className="bot-status-line">
								<span className={`status-dot ${bot.online ? 'online' : ''}`} />
								{bot.name}: {bot.online ? 'オンライン' : 'オフライン'}
							</span>
						</div>
					</IonTitle>
				</IonToolbar>
			</IonHeader>
			<IonContent fullscreen color="light">
				<div className="home-search">
					<IonSearchbar
						placeholder="検索"
						value={query}
						debounce={0}
						onIonInput={(e) => setQuery(e.detail.value ?? '')}
					/>
				</div>

				{results !== null ? (
					<IonList inset>
						<IonListHeader>
							<IonLabel>検索結果</IonLabel>
							{searching && <IonSpinner name="dots" />}
						</IonListHeader>
						<IonItemGroup>
							{results.length === 0 && !searching ? (
								<IonItem>
									<IonLabel color="medium">見つからなかった</IonLabel>
								</IonItem>
							) : (
								results.map((r) => {
									const threadTs = r.message.thread_ts || r.message.ts
									return (
										<IonItem
											key={`${r.channel_id}-${r.message.ts}`}
											button
											detail={false}
											routerLink={`/channels/${encodeURIComponent(r.channel_id)}/threads/${encodeURIComponent(threadTs)}`}
											className="search-result-item"
										>
											<IonLabel>
												<h3>
													{r.is_im ? r.channel_name : `# ${r.channel_name}`}
													<span className="search-result-author"> · {r.message.username}</span>
												</h3>
												<p className="search-result-text">{stripMarkup(r.message.text)}</p>
											</IonLabel>
											<IonNote slot="end">{formatMessageTime(r.message.created_at)}</IonNote>
										</IonItem>
									)
								})
							)}
						</IonItemGroup>
					</IonList>
				) : (
					<>
						<IonList inset>
							<IonListHeader>
								<IonLabel>チャンネル</IonLabel>
							</IonListHeader>
							<IonItemGroup>
								{publicChannels.map((ch) => (
									<IonItem
										key={ch.id}
										button
										detail={false}
										routerLink={`/channels/${encodeURIComponent(ch.id)}`}
										className={ch.unread_count > 0 ? 'channel-item unread' : 'channel-item'}
									>
										<span slot="start" className="channel-hash" aria-hidden="true">
											#
										</span>
										<IonLabel>{ch.name}</IonLabel>
										{ch.unread_count > 0 && (
											<IonBadge slot="end" color="danger">
												{ch.unread_count}
											</IonBadge>
										)}
									</IonItem>
								))}
								{publicChannels.length === 0 && (
									<IonItem>
										<IonLabel color="medium">チャンネルはまだない</IonLabel>
									</IonItem>
								)}
							</IonItemGroup>
						</IonList>

						<IonList inset>
							<IonListHeader>
								<IonLabel>ダイレクトメッセージ</IonLabel>
							</IonListHeader>
							<IonItemGroup>
								{dms.map((ch) => (
									<IonItem
										key={ch.id}
										button
										detail={false}
										routerLink={`/channels/${encodeURIComponent(ch.id)}`}
										className={ch.unread_count > 0 ? 'channel-item unread' : 'channel-item'}
									>
										<div slot="start" className="dm-avatar" aria-hidden="true">
											<IonIcon icon={hardwareChip} />
											<span className={`dm-presence ${bot.online ? 'online' : ''}`} />
										</div>
										<IonLabel>{ch.name || bot.name}</IonLabel>
										{ch.unread_count > 0 && (
											<IonBadge slot="end" color="danger">
												{ch.unread_count}
											</IonBadge>
										)}
									</IonItem>
								))}
								{dms.length === 0 && (
									<IonItem>
										<IonLabel color="medium">DM はまだない</IonLabel>
									</IonItem>
								)}
							</IonItemGroup>
						</IonList>
					</>
				)}
			</IonContent>
		</IonPage>
	)
}
