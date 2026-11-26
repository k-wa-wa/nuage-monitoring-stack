import { useState, type FormEvent } from 'react'
import { Lock, ArrowRight } from 'lucide-react'

interface LoginScreenProps {
	onLoginSuccess: () => Promise<void>
	apiBase: string
}

// 合言葉によるログイン画面である。
export default function LoginScreen({ onLoginSuccess, apiBase }: LoginScreenProps) {
	const [passphrase, setPassphrase] = useState('')
	const [error, setError] = useState<string | null>(null)
	const [submitting, setSubmitting] = useState(false)

	const handleSubmit = async (e: FormEvent) => {
		e.preventDefault()
		if (!passphrase.trim() || submitting) return

		setSubmitting(true)
		setError(null)
		try {
			const res = await fetch(`${apiBase}/api/auth/login`, {
				method: 'POST',
				credentials: 'include',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ passphrase })
			})
			if (!res.ok) {
				setError('合言葉が正しくない。')
				return
			}
			setPassphrase('')
			await onLoginSuccess()
		} catch {
			setError('サーバーへの接続に失敗した。')
		} finally {
			setSubmitting(false)
		}
	}

	return (
		<div className="login-shell">
			<div className="login-card">
				<div className="login-brand">
					<span className="app-logo" aria-hidden="true" />
					<div>
						<h1 className="login-title">Nuage Notify</h1>
						<p className="login-subtitle">クラスタ通知 & Bot チャット</p>
					</div>
				</div>

				<form onSubmit={handleSubmit} className="login-form">
					<label htmlFor="passphrase-input" className="login-label">
						<Lock size={14} />
						合言葉
					</label>
					<input
						id="passphrase-input"
						type="password"
						value={passphrase}
						onChange={(e) => setPassphrase(e.target.value)}
						placeholder="合言葉を入力..."
						autoComplete="current-password"
						autoFocus
						className="login-input"
					/>
					{error && (
						<div className="login-error" role="alert">
							{error}
						</div>
					)}
					<button
						type="submit"
						disabled={submitting || !passphrase.trim()}
						className="btn btn-primary login-submit"
					>
						{submitting ? '認証中...' : 'ログイン'}
						<ArrowRight size={16} />
					</button>
				</form>
			</div>
		</div>
	)
}
