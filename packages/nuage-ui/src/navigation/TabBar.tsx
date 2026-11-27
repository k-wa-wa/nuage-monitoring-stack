import type { ReactNode } from 'react'
import { Badge } from '../components/Badge'

export interface TabBarItem {
	key: string
	label: string
	icon: ReactNode
	badge?: number
	active: boolean
	onSelect: () => void
}

/** 画面下のガラスのタブバーである。 */
export function TabBar({ items }: { items: TabBarItem[] }) {
	return (
		<nav className="nui-tabbar" aria-label="タブ">
			<div className="nui-tabbar-inner">
				{items.map((item) => (
					<button
						key={item.key}
						type="button"
						className="nui-tabbar-item nui-pressable"
						data-active={item.active ? 'true' : 'false'}
						aria-current={item.active ? 'page' : undefined}
						data-tab={item.key}
						onClick={item.onSelect}
					>
						<span className="nui-tabbar-icon" aria-hidden="true">
							{item.icon}
							{item.badge ? <Badge count={item.badge} className="nui-tabbar-badge" /> : null}
						</span>
						<span className="nui-tabbar-label">{item.label}</span>
					</button>
				))}
			</div>
		</nav>
	)
}
