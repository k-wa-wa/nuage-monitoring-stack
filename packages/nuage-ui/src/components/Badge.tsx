import type { ReactNode } from 'react'

/** 未読数などの数のバッジである。0 以下なら出さない。99 を超えたら 99+。 */
export function Badge({ count, className }: { count: number; className?: string }) {
	if (count <= 0) return null
	return (
		<span className={`nui-badge ${className ?? ''}`.trim()} aria-label={`${count} 件`}>
			{count > 99 ? '99+' : count}
		</span>
	)
}

export type Tone = 'neutral' | 'accent' | 'success' | 'warn' | 'blocked'

/** 状態を表す小さなチップである。 */
export function Chip({ tone = 'neutral', children }: { tone?: Tone; children: ReactNode }) {
	return (
		<span className="nui-chip" data-tone={tone}>
			{children}
		</span>
	)
}
