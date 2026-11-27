import type { ReactNode } from 'react'

/** 読み込み中の印である。 */
export function Spinner({ size = 20, label = '読み込み中' }: { size?: number; label?: string }) {
	return <span className="nui-spinner" role="status" aria-label={label} style={{ width: size, height: size }} />
}

/** 中身が無い時の案内である。 */
export function EmptyState({ children }: { children: ReactNode }) {
	return <div className="nui-empty">{children}</div>
}
