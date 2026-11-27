import { ArrowDown } from 'lucide-react'

// 上を読んでいる間に新着が来た時に出す「新着あり」ボタンである。
export default function NewMessagesButton({ visible, onClick }: { visible: boolean; onClick: () => void }) {
	if (!visible) return null
	return (
		<div className="new-messages-anchor">
			<button type="button" className="new-messages-btn nui-pressable" onClick={onClick}>
				<ArrowDown size={14} aria-hidden="true" />
				新着あり
			</button>
		</div>
	)
}
