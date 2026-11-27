import { useCallback, useEffect, useState } from 'react'
import { Button, ListItem, ListSection, Modal, Page, Toggle, useToast } from '@nuage/ui'
import { API_BASE, useChatActions, useChatState } from '../state/chatStore'
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
	const user = useChatState((s) => s.user)
	const bot = useChatState((s) => s.bot)
	const { logout } = useChatActions()
	const toast = useToast()
	const [subscribed, setSubscribed] = useState(false)
	const [busy, setBusy] = useState(false)
	const [logs, setLogs] = useState<string[]>([])
	const [confirmLogout, setConfirmLogout] = useState(false)

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
				toast('通知を受け取るように登録した。', 'success')
			} else {
				await unsubscribePush(API_BASE)
				setSubscribed(false)
				toast('通知の登録を解除した。')
			}
		} catch (err: unknown) {
			const message = err instanceof Error ? err.message : String(err)
			log(`エラー: ${message}`)
			toast(message, 'warn')
		} finally {
			setBusy(false)
		}
	}

	const handleTest = async () => {
		try {
			await sendTestNotify(API_BASE)
			toast('テスト通知の送信を頼んだ。', 'success')
		} catch (err: unknown) {
			const message = err instanceof Error ? err.message : String(err)
			log(`エラー: ${message}`)
			toast(message, 'warn')
		}
	}

	const handleCopy = async () => {
		try {
			await navigator.clipboard.writeText(WEBHOOK_CURL_EXAMPLE)
			toast('コピーした')
		} catch {
			toast('コピーできなかった。', 'warn')
		}
	}

	return (
		<Page title="設定">
			<ListSection title="アカウント">
				<ListItem title="名前" end={<span className="settings-value">{user?.display_name}</span>} />
				<ListItem title={bot.name} end={<span className="settings-value">{bot.online ? 'オンライン' : 'オフライン'}</span>} />
			</ListSection>

			<ListSection
				title="通知"
				footer={
					isPushSupported()
						? undefined
						: 'このブラウザは Web Push に対応していない。iPhone ではホーム画面に追加して開くと使える。'
				}
			>
				<ListItem
					title="プッシュ通知を受け取る"
					end={
						<Toggle
							label="プッシュ通知を受け取る"
							checked={subscribed}
							disabled={busy || !isPushSupported()}
							onChange={(v) => void togglePush(v)}
						/>
					}
				/>
				<ListItem title={<span className="settings-link">テスト通知を送る</span>} onClick={() => void handleTest()} />
			</ListSection>

			<ListSection title="Webhook で通知を送る">
				<div className="settings-code nui-selectable">
					<pre className="code-sample">{WEBHOOK_CURL_EXAMPLE}</pre>
				</div>
				<ListItem title={<span className="settings-link">コピー</span>} onClick={() => void handleCopy()} />
			</ListSection>

			<ListSection title="開発">
				<ListItem
					title="画面の計測表示"
					end={
						<Toggle label="画面の計測表示" checked={isViewportDebugEnabled()} onChange={(v) => setViewportDebugEnabled(v)} />
					}
				/>
			</ListSection>

			{logs.length > 0 && (
				<ListSection title="ログ">
					{logs.map((l, i) => (
						<ListItem key={i} title={<span className="log-line">{l}</span>} />
					))}
				</ListSection>
			)}

			<div className="logout-area">
				<Button variant="danger" block onClick={() => setConfirmLogout(true)}>
					ログアウト
				</Button>
			</div>

			<Modal
				open={confirmLogout}
				onClose={() => setConfirmLogout(false)}
				title="ログアウトする？"
				footer={
					<>
						<Button onClick={() => setConfirmLogout(false)}>キャンセル</Button>
						<Button variant="danger" onClick={() => void logout()}>
							ログアウト
						</Button>
					</>
				}
			>
				<p className="settings-note">もう一度使うには合言葉が要る。</p>
			</Modal>
		</Page>
	)
}
