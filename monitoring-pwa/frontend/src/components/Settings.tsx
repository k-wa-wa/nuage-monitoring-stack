import { Settings as SettingsIcon, Bell, Copy, Check } from 'lucide-react'

interface SettingsProps {
	isSubscribed: boolean
	submitting: boolean
	onSubscribeToggle: () => void
	onSendTestNotify: () => void
	copyWebhookCommand: () => void
	copied: boolean
	logs: string[]
}

// Settings コンポーネントである。プッシュ通知購読、テスト通知、Webhookコピー、ログ表示をまとめる。
export default function Settings({
	isSubscribed,
	submitting,
	onSubscribeToggle,
	onSendTestNotify,
	copyWebhookCommand,
	copied,
	logs
}: SettingsProps) {
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
							<span className="label-desc">この端末でクラスタのアラートを受信する。</span>
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
						GitHub Actions や任意の Cron ジョブ等から、以下のエンドポイントを叩くことでプッシュ通知をトリガーできる。
					</span>
					<div className="webhook-code-block">
						<pre>
							{`curl -X POST https://monitoring.cluster.wpc/webhook/generic \\
  -H "Content-Type: application/json" \\
  -d '{"title": "エラータイトル", "body": "エラーメッセージ", "level": "error", "details": "詳細ログまたはスタックトレース"}'`}
						</pre>
						<button onClick={copyWebhookCommand} className="copy-btn">
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
