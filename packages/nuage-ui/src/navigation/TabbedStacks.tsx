import { useCallback, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { useLocation, useNavigate, type RouteObject } from 'react-router-dom'
import { StackView, type NuageNav } from './StackView'
import { TabBar } from './TabBar'

export interface TabDefinition {
	key: string
	label: string
	icon: ReactNode
	badge?: number
	/** このタブが受け持つ URL の先頭 (例: /tabs/home) */
	basePath: string
	/** このタブの中の画面の割り当て (絶対パス) */
	routes: RouteObject[]
	/** 直接開いた URL から戻る先 */
	parentPath?: (pathname: string) => string | null
}

interface TabbedStacksProps {
	tabs: TabDefinition[]
}

/**
 * 下のタブバーと、タブごとの StackView (タブごとに履歴を持つ) をまとめる。
 * 見えていないタブも DOM に残す (戻った時にスクロール位置や入力が残る)。
 * 画面を押し込んでいる間 (2 枚目以降) はタブバーを隠す (iOS と同じ)。
 */
export function TabbedStacks({ tabs }: TabbedStacksProps) {
	const { pathname } = useLocation()
	const navigate = useNavigate()
	const topPaths = useRef<Record<string, string>>({})
	const [depths, setDepths] = useState<Record<string, number>>({})

	const activeTab = tabs.find((t) => pathname === t.basePath || pathname.startsWith(t.basePath + '/'))
	const showTabBar = !activeTab || (depths[activeTab.key] ?? 1) <= 1

	const onTopChange = useCallback((key: string, path: string, depth: number) => {
		topPaths.current[key] = path
		setDepths((prev) => (prev[key] === depth ? prev : { ...prev, [key]: depth }))
	}, [])
	const topChangeHandlers = useRef<Record<string, (path: string, depth: number) => void>>({})
	const handlerFor = (key: string) =>
		(topChangeHandlers.current[key] ??= (path: string, depth: number) => onTopChange(key, path, depth))

	const select = (tab: TabDefinition) => {
		if (tab === activeTab) {
			navigate(tab.basePath, { state: { nuageNav: 'tab-root' satisfies NuageNav } })
			return
		}
		navigate(topPaths.current[tab.key] ?? tab.basePath, { state: { nuageNav: 'tab' satisfies NuageNav } })
	}

	const style = {
		'--nui-tabbar-space': showTabBar ? 'calc(var(--tabbar-height) + var(--safe-bottom))' : '0px'
	} as CSSProperties

	return (
		<div className="nui-tabs" style={style}>
			{tabs.map((tab) => (
				<div key={tab.key} className="nui-tab-pane" data-active={tab === activeTab ? 'true' : 'false'}>
					<StackView
						basePath={tab.basePath}
						routes={tab.routes}
						parentPath={tab.parentPath}
						active={tab === activeTab}
						onTopChange={handlerFor(tab.key)}
					/>
				</div>
			))}
			{showTabBar && (
				<TabBar
					items={tabs.map((t) => ({
						key: t.key,
						label: t.label,
						icon: t.icon,
						badge: t.badge,
						active: t === activeTab,
						onSelect: () => select(t)
					}))}
				/>
			)}
		</div>
	)
}
