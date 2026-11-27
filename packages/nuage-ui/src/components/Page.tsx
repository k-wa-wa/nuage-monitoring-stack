import type { ReactNode, Ref, UIEventHandler } from 'react'
import { Toolbar } from './Toolbar'

interface PageProps {
	title?: ReactNode
	subtitle?: ReactNode
	/** ツールバーの左 (戻るボタンなど) */
	start?: ReactNode
	/** ツールバーの右 */
	end?: ReactNode
	/** ツールバーの下に固定して出すもの (絞り込み・検索など) */
	headerExtra?: ReactNode
	/** 画面の下に固定して出すもの (送信欄など)。キーボードが開いている間はセーフエリアの余白を外す */
	footer?: ReactNode
	children: ReactNode
	scrollRef?: Ref<HTMLDivElement>
	onScroll?: UIEventHandler<HTMLDivElement>
	className?: string
}

/**
 * 1 画面の骨組みである。ガラスのツールバーは本文の上に重ね、本文はその下をくぐってスクロールする。
 * スクロールは本文の中だけで起こり、外 (画面全体) へ伝わらない。
 */
export function Page({ title, subtitle, start, end, headerExtra, footer, children, scrollRef, onScroll, className }: PageProps) {
	return (
		<div className={`nui-page ${className ?? ''}`.trim()}>
			<div ref={scrollRef} className="nui-page-body nui-scroll" onScroll={onScroll}>
				<Toolbar title={title} subtitle={subtitle} start={start} end={end} extra={headerExtra} />
				<div className="nui-page-content">{children}</div>
			</div>
			{footer ? <div className="nui-page-footer">{footer}</div> : null}
		</div>
	)
}
