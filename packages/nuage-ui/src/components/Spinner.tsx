import type { ReactNode } from 'react'

/** 読み込み中の印である。 */
export function Spinner({ size = 20, label = '読み込み中' }: { size?: number; label?: string }) {
	return <span className="nui-spinner" role="status" aria-label={label} style={{ width: size, height: size }} />
}

interface LoadingStateProps {
	size?: number
	label?: string
	message?: ReactNode
	/** 親の残り領域を埋めて上下左右の中央に置く */
	fill?: boolean
	className?: string
}

/** 領域の中央に読み込み中のスピナーと任意の案内を出す。 */
export function LoadingState({ size = 24, label = '読み込み中', message, fill = false, className }: LoadingStateProps) {
	return (
		<div
			className={`nui-loading-state ${className ?? ''}`.trim()}
			data-fill={fill ? 'true' : 'false'}
			role="status"
			aria-label={typeof message === 'string' ? message : label}
		>
			<Spinner size={size} label={label} />
			{message ? <p className="nui-loading-message">{message}</p> : null}
		</div>
	)
}

/** 中身が無い時の案内である。 */
export function EmptyState({ children }: { children: ReactNode }) {
	return <div className="nui-empty">{children}</div>
}
