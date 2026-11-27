import type { ReactNode, Ref, UIEventHandler } from 'react'
import { LoadingState } from './Spinner'
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
	/** 読み込み中のとき本文の代わりに中央スピナーを出す */
	loading?: boolean
	loadingMessage?: ReactNode
	children: ReactNode
	scrollRef?: Ref<HTMLDivElement>
	onScroll?: UIEventHandler<HTMLDivElement>
	className?: string
}

/**
 * 1 画面の骨組みである。ガラスのツールバーは本文の上に重ね、本文はその下をくぐってスクロールする。
 * スクロールは本文の中だけで起こり、外 (画面全体) へ伝わらない。
 */
export function Page({
	title,
	subtitle,
	start,
	end,
	headerExtra,
	footer,
	loading = false,
	loadingMessage,
	children,
	scrollRef,
	onScroll,
	className
}: PageProps) {
	return (
		<div className={`nui-page ${className ?? ''}`.trim()}>
			<div ref={scrollRef} className="nui-page-body nui-scroll" onScroll={onScroll}>
				<Toolbar title={title} subtitle={subtitle} start={start} end={end} extra={headerExtra} />
				<div className="nui-page-content">
					{loading ? <LoadingState message={loadingMessage} fill /> : children}
				</div>
			</div>
			{footer ? <div className="nui-page-footer">{footer}</div> : null}
		</div>
	)
}
