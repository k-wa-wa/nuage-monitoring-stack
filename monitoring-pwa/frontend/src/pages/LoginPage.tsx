import { useState, type FormEvent } from 'react'
import { Button, Card, TextField } from '@nuage/ui'
import { API_BASE, useChatActions } from '../state/chatStore'

export default function LoginPage() {
	const { reloadBootstrap } = useChatActions()
	const [passphrase, setPassphrase] = useState('')
	const [submitting, setSubmitting] = useState(false)
	const [error, setError] = useState<string | null>(null)

	const handleSubmit = async (e: FormEvent) => {
		e.preventDefault()
		if (!passphrase.trim() || submitting) return
		setSubmitting(true)
		setError(null)
		try {
			const res = await fetch(`${API_BASE}/api/auth/login`, {
				method: 'POST',
				credentials: 'include',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ passphrase })
			})
			if (!res.ok) {
				setError(
					res.status === 429
						? 'ログインの試行が多すぎる。少し待ってからもう一度試してほしい。'
						: res.status === 401
							? '合言葉が正しくない。'
							: `ログインに失敗した (HTTP ${res.status})。`
				)
				return
			}
			setPassphrase('')
			await reloadBootstrap()
		} catch {
			setError('サーバーにつながらなかった。')
		} finally {
			setSubmitting(false)
		}
	}

	return (
		<div className="login-screen nui-scroll">
			<Card className="login-card">
				<form onSubmit={(e) => void handleSubmit(e)}>
					<div className="login-brand">
						<span className="login-logo" aria-hidden="true" />
						<div>
							<h1 className="login-title app-title-tap">Nuage Notify</h1>
							<p className="login-subtitle">クラスタの通知と bot のチャット</p>
						</div>
					</div>
					<TextField
						id="passphrase-input"
						label="合言葉"
						type="password"
						placeholder="合言葉を入力"
						autoComplete="current-password"
						value={passphrase}
						error={error}
						onChange={(e) => setPassphrase(e.target.value)}
					/>
					<Button type="submit" variant="primary" block loading={submitting} disabled={!passphrase.trim()}>
						ログイン
					</Button>
				</form>
			</Card>
		</div>
	)
}
