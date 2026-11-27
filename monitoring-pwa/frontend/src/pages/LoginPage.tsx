import { useState, type FormEvent } from 'react'
import { Button, Card, TextField } from '@nuage/ui'
import { useLogin } from '../hooks/useLogin'

export default function LoginPage() {
	const { submitting, error, login } = useLogin()
	const [passphrase, setPassphrase] = useState('')

	const handleSubmit = async (e: FormEvent) => {
		e.preventDefault()
		if (await login(passphrase)) {
			setPassphrase('')
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
