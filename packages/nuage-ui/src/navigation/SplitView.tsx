import type { ReactNode } from 'react'

interface SplitViewProps {
	/** 左の列 (一覧・ナビゲーション) */
	sidebar: ReactNode
	/** 真ん中 (今開いている画面) */
	main: ReactNode
	/** 右の列 (スレッドや詳細)。無ければ出さない */
	aside?: ReactNode
}

/** PC・タブレットで、一覧と中身 (と詳細) を左右に並べる。 */
export function SplitView({ sidebar, main, aside }: SplitViewProps) {
	return (
		<div className="nui-split" data-has-aside={aside ? 'true' : 'false'}>
			<aside className="nui-split-sidebar">{sidebar}</aside>
			<main className="nui-split-main">{main}</main>
			{aside ? <section className="nui-split-aside">{aside}</section> : null}
		</div>
	)
}
