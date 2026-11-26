import { useCallback, useEffect, useState } from 'react'
import {
	IonButton,
	IonContent,
	IonHeader,
	IonItem,
	IonItemGroup,
	IonLabel,
	IonList,
	IonListHeader,
	IonNote,
	IonPage,
	IonTitle,
	IonToggle,
	IonToolbar,
	useIonAlert
} from '@ionic/react'
import { API_BASE, useChat } from '../state/chatStore'
import {
	getCurrentSubscription,
	isPushSupported,
	sendTestNotify,
	subscribePush,
	unsubscribePush
} from '../utils/pushSubscription'
import { isViewportDebugEnabled, setViewportDebugEnabled } from '../utils/viewportDebug'

const WEBHOOK_CURL_EXAMPLE = `curl -X POST https://monitoring.wpcapp.net/webhook/generic \\
  -H "Content-Type: application/json" \\
  -d '{"title": "デプロイ完了", "body": "アプリを更新した。", "level": "info", "url": "https://argocd.wpcapp.net", "details": "revision: abc1234"}'`

export default function SettingsPage() {
	const { user, bot, logout } = useChat()
	const [subscribed, setSubscribed] = useState(false)
	const [busy, setBusy] = useState(false)
	const [logs, setLogs] = useState<string[]>([])
	const [copied, setCopied] = useState(false)
	const [presentAlert] = useIonAlert()

	const log = useCallback((msg: string) => {
		setLogs((prev) => [`[${new Date().toLocaleTimeString()}] ${msg}`, ...prev].slice(0, 50))
	}, [])

	useEffect(() => {
		getCurrentSubscription(API_BASE, log)
			.then((sub) => setSubscribed(Boolean(sub)))
			.catch((err: unknown) => log(`購読の確認に失敗した: ${err instanceof Error ? err.message : String(err)}`))
	}, [log])

	const togglePush = async (enable: boolean) => {
		setBusy(true)
		try {
			if (enable) {
				await subscribePush(API_BASE)
				setSubscribed(true)
				log('通知を受け取るように登録した。')
			} else {
				await unsubscribePush(API_BASE)
				setSubscribed(false)
				log('通知の登録を解除した。')
			}
		} catch (err: unknown) {
			log(`エラー: ${err instanceof Error ? err.message : String(err)}`)
		} finally {
			setBusy(false)
		}
	}

	const handleTest = async () => {
		try {
			await sendTestNotify(API_BASE)
			log('テスト通知の送信を頼んだ。')
		} catch (err: unknown) {
			log(`エラー: ${err instanceof Error ? err.message : String(err)}`)
		}
	}

	const handleCopy = async () => {
		try {
			await navigator.clipboard.writeText(WEBHOOK_CURL_EXAMPLE)
			setCopied(true)
			window.setTimeout(() => setCopied(false), 1500)
		} catch {
			log('コピーできなかった。')
		}
	}

	const confirmLogout = () =>
		presentAlert({
			header: 'ログアウトする？',
			buttons: [
				{ text: 'キャンセル', role: 'cancel' },
				{ text: 'ログアウト', role: 'destructive', handler: () => void logout() }
			]
		})

	return (
		<IonPage>
			<IonHeader translucent>
				<IonToolbar>
					<IonTitle>設定</IonTitle>
				</IonToolbar>
			</IonHeader>
			<IonContent fullscreen color="light">
				<IonHeader collapse="condense">
					<IonToolbar color="light">
						<IonTitle size="large">設定</IonTitle>
					</IonToolbar>
				</IonHeader>

				<IonList inset>
					<IonListHeader>
						<IonLabel>アカウント</IonLabel>
					</IonListHeader>
					<IonItemGroup>
						<IonItem>
							<IonLabel>名前</IonLabel>
							<IonNote slot="end">{user?.display_name}</IonNote>
						</IonItem>
						<IonItem>
							<IonLabel>{bot.name}</IonLabel>
							<IonNote slot="end">{bot.online ? 'オンライン' : 'オフライン'}</IonNote>
						</IonItem>
					</IonItemGroup>
				</IonList>

				<IonList inset>
					<IonListHeader>
						<IonLabel>通知</IonLabel>
					</IonListHeader>
					<IonItemGroup>
						<IonItem>
							<IonToggle
								checked={subscribed}
								disabled={busy || !isPushSupported()}
								onIonChange={(e) => void togglePush(e.detail.checked)}
							>
								プッシュ通知を受け取る
							</IonToggle>
						</IonItem>
						<IonItem button detail={false} onClick={() => void handleTest()}>
							<IonLabel color="primary">テスト通知を送る</IonLabel>
						</IonItem>
					</IonItemGroup>
				</IonList>
				{!isPushSupported() && (
					<p className="list-footnote">
						このブラウザは Web Push に対応していない。iPhone ではホーム画面に追加して開くと使える。
					</p>
				)}

				<IonList inset>
					<IonListHeader>
						<IonLabel>Webhook で通知を送る</IonLabel>
					</IonListHeader>
					<IonItemGroup>
						<IonItem lines="none">
							<pre className="code-sample">{WEBHOOK_CURL_EXAMPLE}</pre>
						</IonItem>
						<IonItem button detail={false} onClick={() => void handleCopy()}>
							<IonLabel color="primary">{copied ? 'コピーした' : 'コピー'}</IonLabel>
						</IonItem>
					</IonItemGroup>
				</IonList>

				<IonList inset>
					<IonListHeader>
						<IonLabel>開発</IonLabel>
					</IonListHeader>
					<IonItemGroup>
						<IonItem>
							<IonToggle
								checked={isViewportDebugEnabled()}
								onIonChange={(e) => setViewportDebugEnabled(e.detail.checked)}
							>
								画面の計測表示
							</IonToggle>
						</IonItem>
					</IonItemGroup>
				</IonList>

				{logs.length > 0 && (
					<IonList inset>
						<IonListHeader>
							<IonLabel>ログ</IonLabel>
						</IonListHeader>
						<IonItemGroup>
							{logs.map((l, i) => (
								<IonItem key={i} lines="inset">
									<IonLabel className="ion-text-wrap log-line">{l}</IonLabel>
								</IonItem>
							))}
						</IonItemGroup>
					</IonList>
				)}

				<div className="logout-area">
					<IonButton expand="block" color="danger" fill="clear" onClick={() => void confirmLogout()}>
						ログアウト
					</IonButton>
				</div>
			</IonContent>
		</IonPage>
	)
}
