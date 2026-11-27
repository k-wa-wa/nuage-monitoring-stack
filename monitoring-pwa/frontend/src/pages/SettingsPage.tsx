import { useState } from 'react'
import { Button, ListItem, ListSection, Modal, Page, Toggle, useToast } from '@nuage/ui'
import { usePushSettings } from '../hooks/usePushSettings'
import { useChatActions, useChatState } from '../state/chatStore'
import { isViewportDebugEnabled, setViewportDebugEnabled } from '../utils/viewportDebug'

const WEBHOOK_CURL_EXAMPLE = `curl -X POST https://monitoring.wpcapp.net/webhook/generic \\
  -H "Content-Type: application/json" \\
  -d '{"title": "デプロイ完了", "body": "アプリを更新した。", "level": "info", "url": "https://argocd.wpcapp.net", "details": "revision: abc1234"}'`

export default function SettingsPage() {
	const user = useChatState((s) => s.user)
	const bot = useChatState((s) => s.bot)
	const { logout } = useChatActions()
	const toast = useToast()
	const { pushSupported, subscribed, checking, busy, sendingTest, logs, togglePush, sendTest } = usePushSettings(toast)
	const [confirmLogout, setConfirmLogout] = useState(false)
	const [loggingOut, setLoggingOut] = useState(false)

	const handleCopy = async () => {
		try {
			await navigator.clipboard.writeText(WEBHOOK_CURL_EXAMPLE)
			toast('コピーした')
		} catch {
			toast('コピーできなかった。', 'warn')
		}
	}

	const handleLogout = async () => {
		if (loggingOut) return
		setLoggingOut(true)
		try {
			await logout()
		} finally {
			setLoggingOut(false)
		}
	}

	return (
		<Page title="設定">
			<ListSection title="アカウント">
				<ListItem title="名前" loading={!user} end={<span className="settings-value">{user?.display_name}</span>} />
				<ListItem title={bot.name} end={<span className="settings-value">{bot.online ? 'オンライン' : 'オフライン'}</span>} />
			</ListSection>

			<ListSection
				title="通知"
				footer={
					pushSupported
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
							loading={checking || busy}
							disabled={!pushSupported}
							onChange={(v) => void togglePush(v)}
						/>
					}
				/>
				<ListItem
					title={<span className="settings-link">テスト通知を送る</span>}
					loading={sendingTest}
					onClick={() => void sendTest()}
				/>
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
				onClose={() => {
					if (!loggingOut) setConfirmLogout(false)
				}}
				title="ログアウトする？"
				footer={
					<>
						<Button disabled={loggingOut} onClick={() => setConfirmLogout(false)}>
							キャンセル
						</Button>
						<Button variant="danger" loading={loggingOut} onClick={() => void handleLogout()}>
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
