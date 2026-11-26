import { IonIcon } from '@ionic/react'
import { arrowDown } from 'ionicons/icons'

// 上を読んでいる間に新着が来た時に出す「新着あり」ボタンである。
export default function NewMessagesButton({ visible, onClick }: { visible: boolean; onClick: () => void }) {
	if (!visible) return null
	return (
		<div slot="fixed" className="new-messages-anchor">
			<button type="button" className="new-messages-btn" onClick={onClick}>
				<IonIcon icon={arrowDown} aria-hidden="true" />
				新着あり
			</button>
		</div>
	)
}
