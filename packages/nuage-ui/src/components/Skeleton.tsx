import type { CSSProperties } from 'react'

interface SkeletonProps {
	width?: number | string
	height?: number | string
	variant?: 'text' | 'rect' | 'circle'
	className?: string
}

/** 読み込み中のプレースホルダーである。 */
export function Skeleton({ width, height, variant = 'text', className }: SkeletonProps) {
	const style: CSSProperties = {}
	if (width !== undefined) style.width = width
	if (height !== undefined) style.height = height
	return (
		<span
			className={`nui-skeleton ${className ?? ''}`.trim()}
			data-variant={variant}
			style={style}
			aria-hidden="true"
		/>
	)
}
