import { useState, type FormEvent } from 'react'
import { IonButton, IonContent, IonInput, IonItem, IonItemGroup, IonList, IonPage, IonSpinner } from '@ionic/react'
import { API_BASE, useChat } from '../state/chatStore'

export default function LoginPage() {
	const { reloadBootstrap } = useChat()
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
		<IonPage>
			<IonContent fullscreen className="login-content">
				<form className="login-form" onSubmit={(e) => void handleSubmit(e)}>
					<div className="login-logo" aria-hidden="true" />
					<h1 className="login-title app-title-tap">Nuage Notify</h1>
					<p className="login-subtitle">クラスタの通知と bot のチャット</p>
					<IonList inset>
						<IonItemGroup>
							<IonItem>
								<IonInput
									id="passphrase-input"
									type="password"
									label="合言葉"
									labelPlacement="stacked"
									placeholder="合言葉を入力"
									autocomplete="current-password"
									value={passphrase}
									onIonInput={(e) => setPassphrase(String(e.detail.value ?? ''))}
								/>
							</IonItem>
						</IonItemGroup>
					</IonList>
					{error && (
						<p className="login-error" role="alert">
							{error}
						</p>
					)}
					<div className="login-actions">
						<IonButton type="submit" expand="block" disabled={!passphrase.trim() || submitting}>
							{submitting ? <IonSpinner name="crescent" /> : 'ログイン'}
						</IonButton>
					</div>
				</form>
			</IonContent>
		</IonPage>
	)
}
