import { useState, type KeyboardEvent } from 'react'
import { IonButton, IonIcon, IonTextarea } from '@ionic/react'
import { arrowUp } from 'ionicons/icons'

interface ComposerProps {
	placeholder: string
	// 送信に成功したら true を返す。失敗時は入力を残す。
	onSend: (text: string) => Promise<boolean>
	notice?: string | null
	error?: string | null
}

// 物理キーボードのある環境 (PC) では Enter で送信し、Shift+Enter で改行する。
// スマホの画面キーボードでは Enter は改行にして、送信ボタンで送る (Slack と同じ)。
const hasFinePointer = () =>
	typeof window !== 'undefined' &&
	typeof window.matchMedia === 'function' &&
	window.matchMedia('(pointer: fine)').matches

export default function Composer({ placeholder, onSend, notice, error }: ComposerProps) {
	const [draft, setDraft] = useState('')
	const [sending, setSending] = useState(false)
	const canSend = draft.trim().length > 0 && !sending

	const submit = async () => {
		const text = draft.trim()
		if (!text || sending) return
		setSending(true)
		try {
			const ok = await onSend(text)
			if (ok) setDraft('')
		} finally {
			setSending(false)
		}
	}

	const handleKeyDown = (e: KeyboardEvent<HTMLIonTextareaElement>) => {
		if (e.key !== 'Enter' || e.shiftKey || e.nativeEvent.isComposing) return
		if (!hasFinePointer()) return
		e.preventDefault()
		void submit()
	}

	return (
		<div className="composer">
			{error && (
				<div className="composer-error" role="alert">
					{error}
				</div>
			)}
			{notice && !error && <div className="composer-notice">{notice}</div>}
			<div className="composer-row">
				<IonTextarea
					className="composer-input"
					aria-label={placeholder}
					placeholder={placeholder}
					value={draft}
					autoGrow
					rows={1}
					enterkeyhint="enter"
					onIonInput={(e) => setDraft(e.detail.value ?? '')}
					onKeyDown={handleKeyDown}
				/>
				<IonButton
					className="composer-send"
					shape="round"
					disabled={!canSend}
					aria-label="送信"
					onClick={() => void submit()}
				>
					<IonIcon slot="icon-only" icon={arrowUp} />
				</IonButton>
			</div>
		</div>
	)
}
