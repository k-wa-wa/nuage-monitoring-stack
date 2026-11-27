import { useEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { IconButton } from './Button'

interface ModalProps {
	open: boolean
	onClose: () => void
	title: ReactNode
	children: ReactNode
	footer?: ReactNode
}

/**
 * 補助的な詳細・設定を出すモーダルである (規約 4)。スマホ幅では下から出るシートになる。
 * 背景を押す・右上の × ・Esc で閉じる。開いた時に × へ自動でフォーカスして青枠を出すことはしない。
 */
export function Modal({ open, onClose, title, children, footer }: ModalProps) {
	const dialogRef = useRef<HTMLDivElement | null>(null)
	useEffect(() => {
		if (!open) return
		const onKey = (e: KeyboardEvent) => {
			if (e.key === 'Escape') onClose()
		}
		window.addEventListener('keydown', onKey)
		dialogRef.current?.focus({ preventScroll: true })
		return () => window.removeEventListener('keydown', onKey)
	}, [open, onClose])
	if (!open || typeof document === 'undefined') return null
	return createPortal(
		<div className="nui-modal-backdrop" onClick={onClose}>
			<div
				ref={dialogRef}
				className="nui-modal"
				role="dialog"
				aria-modal="true"
				tabIndex={-1}
				onClick={(e) => e.stopPropagation()}
			>
				<div className="nui-modal-header">
					<h2 className="nui-modal-title">{title}</h2>
					<IconButton label="閉じる" icon={<X size={18} aria-hidden="true" />} onClick={onClose} />
				</div>
				<div className="nui-modal-body nui-scroll">{children}</div>
				{footer ? <div className="nui-modal-footer">{footer}</div> : null}
			</div>
		</div>,
		document.body
	)
}
