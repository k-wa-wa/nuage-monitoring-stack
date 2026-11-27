import { useEffect, useState } from 'react'
import { fetchFiringCount } from '../api/activity'
import { useChatState } from '../state/chatStore'

/** 発生中のアラート件数 (タブ・サイドバーのバッジ用) を購読する。 */
export function useFiringCount(): number {
	const activityVersion = useChatState((s) => s.activityVersion)
	const [count, setCount] = useState(0)

	useEffect(() => {
		const controller = new AbortController()
		fetchFiringCount(controller.signal)
			.then((next) => {
				if (!controller.signal.aborted && next !== null) {
					setCount(next)
				}
			})
			.catch(() => {})
		return () => controller.abort()
	}, [activityVersion])

	return count
}
