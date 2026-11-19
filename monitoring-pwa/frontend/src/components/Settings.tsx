import { useState } from 'react'
import { Settings as SettingsIcon, Bell, Copy, Check } from 'lucide-react'

const WEBHOOK_CURL_EXAMPLE = `curl -X POST https://monitoring.wpcapp.net/webhook/generic \\
  -H "Content-Type: application/json" \\
  -d '{"title": "デプロイ完了", "body": "アプリを更新した。", "level": "info", "url": "https://argocd.wpcapp.net", "details": "revision: abc1234"}'`

interface SettingsProps {
	isSubscribed: boolean
	submitting: boolean
	onSubscribeToggle: () => void
	onSendTestNotify: () => void
	logs: string[]
}

// Settings コンポーネントである。プッシュ通知購読、テスト通知、Webhookコピー、ログ表示をまとめる。
export default function Settings({
	isSubscribed,
	submitting,
	onSubscribeToggle,
	onSendTestNotify,
	logs
}: SettingsProps) {
	const [copied, setCopied] = useState(false)

	const copyWebhookCommand = () => {
		navigator.clipboard.writeText(WEBHOOK_CURL_EXAMPLE)
		setCopied(true)
		setTimeout(() => setCopied(false), 2000)
	}

	return (
		<>
			<div className="card">
				<h2 className="card-title">
					<SettingsIcon size={18} />
					通知設定
				</h2>
				<div className="settings-section">
					<div className="settings-row">
						<div className="settings-label">
							<span className="label-title">プッシュ通知</span>
							<span className="label-desc">この端末でクラスタの通知を受信する。</span>
						</div>
						<button
							onClick={onSubscribeToggle}
							disabled={submitting}
							className={`btn ${submitting ? 'btn-disabled' : 'btn-primary'}`}
						>
							{isSubscribed ? '解除する' : '有効にする'}
						</button>
					</div>

					{isSubscribed && (
						<div className="settings-actions">
							<button onClick={onSendTestNotify} className="btn btn-secondary">
								テスト通知を送信
							</button>
						</div>
					)}
				</div>
			</div>

			<div className="card">
				<h2 className="card-title">
					<Bell size={18} />
					汎用 Webhook URL
				</h2>
				<div className="settings-section">
					<span className="label-desc">
						Argo CD・GitHub Actions・Cron ジョブ等から以下のエンドポイントを叩くことでプッシュ通知を送信できる。url を指定すると通知詳細から対象アプリへ遷移できる。
					</span>
					<div className="webhook-code-block">
						<pre>{WEBHOOK_CURL_EXAMPLE}</pre>
						<button onClick={copyWebhookCommand} className="copy-btn" aria-label="Webhookコマンドをコピー">
							{copied ? <Check size={12} /> : <Copy size={12} />}
						</button>
					</div>
				</div>
			</div>

			{logs.length > 0 && (
				<div className="card">
					<h3 className="syslog-title">システムログ</h3>
					<div className="syslog-body">
						{logs.map((logItem, idx) => (
							<div key={idx}>{logItem}</div>
						))}
					</div>
				</div>
			)}
		</>
	)
}
