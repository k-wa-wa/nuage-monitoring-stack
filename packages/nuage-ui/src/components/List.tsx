import type { ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router-dom'

/** 見出しの付いた、角の丸いひとまとまりのリストである。 */
export function ListSection({ title, footer, children }: { title?: ReactNode; footer?: ReactNode; children: ReactNode }) {
	return (
		<section className="nui-list-section">
			{title ? <h2 className="nui-list-title">{title}</h2> : null}
			<div className="nui-list" role="list">
				{children}
			</div>
			{footer ? <p className="nui-list-footer">{footer}</p> : null}
		</section>
	)
}

interface ListItemProps {
	title: ReactNode
	subtitle?: ReactNode
	start?: ReactNode
	end?: ReactNode
	/** 右端に「>」を出す (押すと別の画面へ移る項目) */
	chevron?: boolean
	/** 移り先 (react-router の Link になる) */
	to?: string
	onClick?: () => void
	/** 未読などで強調する */
	emphasized?: boolean
	selected?: boolean
	className?: string
}

/** リストの 1 行である。to か onClick があれば押せる行になる。 */
export function ListItem({ title, subtitle, start, end, chevron, to, onClick, emphasized, selected, className }: ListItemProps) {
	const inner = (
		<>
			{start ? <span className="nui-list-item-start">{start}</span> : null}
			<span className="nui-list-item-body">
				<span className="nui-list-item-title">{title}</span>
				{subtitle ? <span className="nui-list-item-subtitle">{subtitle}</span> : null}
			</span>
			{end ? <span className="nui-list-item-end">{end}</span> : null}
			{chevron ? <ChevronRight className="nui-list-item-chevron" size={16} aria-hidden="true" /> : null}
		</>
	)
	const props = {
		className: `nui-list-item ${to || onClick ? 'nui-pressable' : ''} ${className ?? ''}`.trim(),
		'data-emphasized': emphasized ? 'true' : 'false',
		'data-selected': selected ? 'true' : 'false',
		role: 'listitem'
	}
	if (to) {
		return (
			<Link to={to} {...props} aria-current={selected ? 'page' : undefined}>
				{inner}
			</Link>
		)
	}
	if (onClick) {
		return (
			<button type="button" onClick={onClick} {...props}>
				{inner}
			</button>
		)
	}
	return <div {...props}>{inner}</div>
}
