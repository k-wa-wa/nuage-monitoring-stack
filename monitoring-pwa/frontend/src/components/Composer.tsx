import { useState, type KeyboardEvent } from 'react'
import { IconButton, TextArea } from '@nuage/ui'
import { ArrowUp } from 'lucide-react'

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
	typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(pointer: fine)').matches

export default function Composer({ placeholder, onSend, notice, error }: ComposerProps) {
	const [draft, setDraft] = useState('')
	const [sending, setSending] = useState(false)
	const canSend = draft.trim().length > 0 && !sending

	const submit = async () => {
		const text = draft.trim()
		if (!text || sending) return
		setSending(true)
		try {
			if (await onSend(text)) setDraft('')
		} finally {
			setSending(false)
		}
	}

	const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
		if (e.key !== 'Enter' || e.shiftKey || e.nativeEvent.isComposing || !hasFinePointer()) return
		e.preventDefault()
		void submit()
	}

	return (
		<div className="composer">
			{error ? (
				<div className="composer-error" role="alert">
					{error}
				</div>
			) : notice ? (
				<div className="composer-notice">{notice}</div>
			) : null}
			<div className="composer-row">
				<TextArea
					className="composer-input"
					aria-label={placeholder}
					placeholder={placeholder}
					value={draft}
					maxRows={6}
					enterKeyHint="enter"
					onChange={(e) => setDraft(e.target.value)}
					onKeyDown={handleKeyDown}
				/>
				<IconButton
					label="送信"
					variant="filled"
					className="composer-send"
					loading={sending}
					disabled={!canSend}
					icon={<ArrowUp size={18} aria-hidden="true" />}
					onClick={() => void submit()}
				/>
			</div>
		</div>
	)
}
