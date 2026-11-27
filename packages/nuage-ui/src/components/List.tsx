import type { ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router-dom'
import { Skeleton } from './Skeleton'
import { Spinner } from './Spinner'

/** 見出しの付いた、角の丸いひとまとまりのリストである。 */
export function ListSection({
	title,
	footer,
	loading = false,
	children
}: {
	title?: ReactNode
	footer?: ReactNode
	loading?: boolean
	children: ReactNode
}) {
	return (
		<section className="nui-list-section" aria-busy={loading || undefined}>
			{title || loading ? (
				<div className="nui-list-header">
					{title ? <h2 className="nui-list-title">{title}</h2> : null}
					{loading ? <Spinner size={12} /> : null}
				</div>
			) : null}
			<div className="nui-list" role="list">
				{children}
			</div>
			{footer ? <p className="nui-list-footer">{footer}</p> : null}
		</section>
	)
}

interface ListItemProps {
	title?: ReactNode
	subtitle?: ReactNode
	start?: ReactNode
	end?: ReactNode
	/** 右端に「>」を出す (押すと別の画面へ移る項目) */
	chevron?: boolean
	/** 移り先 (react-router の Link になる) */
	to?: string
	onClick?: () => void
	disabled?: boolean
	/** 値部分やタイトルの未取得状態・処理中を示す */
	loading?: boolean
	/** 未読などで強調する */
	emphasized?: boolean
	selected?: boolean
	className?: string
}

/** リストの 1 行である。to か onClick があれば押せる行になる。 */
export function ListItem({
	title,
	subtitle,
	start,
	end,
	chevron,
	to,
	onClick,
	disabled = false,
	loading = false,
	emphasized,
	selected,
	className
}: ListItemProps) {
	const endContent = loading
		? end !== undefined
			? <Skeleton width={48} height={14} />
			: onClick
				? <Spinner size={14} />
				: null
		: end
	const titleContent =
		title !== undefined && title !== null && title !== '' ? (
			title
		) : loading ? (
			<Skeleton width="60%" height={14} />
		) : null
	const inner = (
		<>
			{start ? <span className="nui-list-item-start">{start}</span> : null}
			<span className="nui-list-item-body">
				<span className="nui-list-item-title">{titleContent}</span>
				{subtitle ? <span className="nui-list-item-subtitle">{subtitle}</span> : null}
			</span>
			{endContent ? <span className="nui-list-item-end">{endContent}</span> : null}
			{chevron ? <ChevronRight className="nui-list-item-chevron" size={16} aria-hidden="true" /> : null}
		</>
	)
	const interactive = Boolean((to || onClick) && !disabled)
	const props = {
		className: `nui-list-item ${interactive ? 'nui-pressable' : ''} ${className ?? ''}`.trim(),
		'data-emphasized': emphasized ? 'true' : 'false',
		'data-selected': selected ? 'true' : 'false',
		'data-loading': loading ? 'true' : 'false',
		'aria-busy': loading || undefined,
		role: 'listitem'
	}
	if (to && !disabled) {
		return (
			<Link to={to} {...props} aria-current={selected ? 'page' : undefined}>
				{inner}
			</Link>
		)
	}
	if (onClick) {
		return (
			<button type="button" onClick={onClick} disabled={disabled || loading} {...props}>
				{inner}
			</button>
		)
	}
	return <div {...props}>{inner}</div>
}
