import type { ReactNode } from 'react'

interface ToolbarProps {
	title?: ReactNode
	subtitle?: ReactNode
	start?: ReactNode
	end?: ReactNode
	extra?: ReactNode
}

/** 画面上のガラスのツールバーである。上端のセーフエリア (ノッチ・ステータスバー) の分だけ下げる。 */
export function Toolbar({ title, subtitle, start, end, extra }: ToolbarProps) {
	return (
		<header className="nui-toolbar">
			<div className="nui-toolbar-row">
				<div className="nui-toolbar-start">{start}</div>
				<div className="nui-toolbar-title">
					{title ? <div className="nui-toolbar-title-main">{title}</div> : null}
					{subtitle ? <div className="nui-toolbar-title-sub">{subtitle}</div> : null}
				</div>
				<div className="nui-toolbar-end">{end}</div>
			</div>
			{extra ? <div className="nui-toolbar-extra">{extra}</div> : null}
		</header>
	)
}
