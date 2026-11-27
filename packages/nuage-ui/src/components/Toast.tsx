import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Tone } from './Badge'

interface ToastItem {
	id: number
	message: ReactNode
	tone: Tone
}

const ToastContext = createContext<((message: ReactNode, tone?: Tone) => void) | null>(null)

/** 画面の上に短く出す知らせ (トースト) を出せるようにする。 */
export function ToastProvider({ children, duration = 2600 }: { children: ReactNode; duration?: number }) {
	const [items, setItems] = useState<ToastItem[]>([])
	const seq = useRef(0)
	const show = useCallback(
		(message: ReactNode, tone: Tone = 'neutral') => {
			const id = ++seq.current
			setItems((prev) => [...prev, { id, message, tone }])
			window.setTimeout(() => setItems((prev) => prev.filter((t) => t.id !== id)), duration)
		},
		[duration]
	)
	const value = useMemo(() => show, [show])
	return (
		<ToastContext.Provider value={value}>
			{children}
			<div className="nui-toasts" aria-live="polite">
				{items.map((t) => (
					<div key={t.id} className="nui-toast" data-tone={t.tone}>
						{t.message}
					</div>
				))}
			</div>
		</ToastContext.Provider>
	)
}

/** トーストを出す関数を返す。ToastProvider の外では使えない。 */
export function useToast(): (message: ReactNode, tone?: Tone) => void {
	const show = useContext(ToastContext)
	if (!show) throw new Error('useToast は ToastProvider の内側で使う')
	return show
}
