import {
	IonBadge,
	IonContent,
	IonHeader,
	IonIcon,
	IonItem,
	IonItemGroup,
	IonLabel,
	IonList,
	IonPage,
	IonTitle,
	IonToolbar
} from '@ionic/react'
import { hardwareChip } from 'ionicons/icons'
import { useChat } from '../state/chatStore'

export default function DmsPage() {
	const { channels, bot } = useChat()
	const dms = channels.filter((c) => c.is_im)

	return (
		<IonPage>
			<IonHeader translucent>
				<IonToolbar>
					<IonTitle>DM</IonTitle>
				</IonToolbar>
			</IonHeader>
			<IonContent fullscreen color="light">
				<IonHeader collapse="condense">
					<IonToolbar color="light">
						<IonTitle size="large">DM</IonTitle>
					</IonToolbar>
				</IonHeader>
				<IonList inset>
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
								<IonLabel>
									<h2>{ch.name || bot.name}</h2>
									<p>{bot.online ? 'オンライン' : 'オフライン'}</p>
								</IonLabel>
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
			</IonContent>
		</IonPage>
	)
}
