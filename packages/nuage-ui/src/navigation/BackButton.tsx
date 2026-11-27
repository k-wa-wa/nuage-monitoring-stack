import { ChevronLeft } from 'lucide-react'
import { useStack } from './StackView'

/** 積まれた画面の左上に置く「戻る」。戻れない時 (いちばん下の画面・PC の横並び) は何も出さない。 */
export function BackButton({ label = '戻る' }: { label?: string }) {
	const stack = useStack()
	if (!stack?.canGoBack) return null
	return (
		<button type="button" className="nui-icon-button nui-back-button nui-pressable" aria-label={label} onClick={() => stack.back()}>
			<ChevronLeft size={24} strokeWidth={2} aria-hidden="true" />
		</button>
	)
}
