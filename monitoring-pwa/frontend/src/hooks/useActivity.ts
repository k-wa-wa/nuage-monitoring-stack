import { useCallback, useEffect, useState } from 'react'
import { fetchActivity } from '../api/activity'
import { useChatState } from '../state/chatStore'
import type { ActivityFilter, ActivityItem } from '../types/chat'

/** 通知一覧の取得・絞り込み・エラー再試行を管理する。 */
export function useActivity() {
	const activityVersion = useChatState((s) => s.activityVersion)
	const [items, setItems] = useState<ActivityItem[] | null>(null)
	const [firingCount, setFiringCount] = useState(0)
	const [filter, setFilter] = useState<ActivityFilter>('all')
	const [loading, setLoading] = useState(true)
	const [error, setError] = useState<string | null>(null)
	const [reloadToken, setReloadToken] = useState(0)

	const changeFilter = useCallback((next: ActivityFilter) => {
		setFilter(next)
		setItems(null)
		setLoading(true)
		setError(null)
	}, [])

	const reload = useCallback(() => {
		setLoading(true)
		setError(null)
		setReloadToken((n) => n + 1)
	}, [])

	useEffect(() => {
		const controller = new AbortController()
		fetchActivity(filter, 100, controller.signal)
			.then((data) => {
				if (controller.signal.aborted) return
				setItems(data.items)
				setFiringCount(data.firing_count)
				setError(null)
				setLoading(false)
			})
			.catch((err: unknown) => {
				if (controller.signal.aborted) return
				console.error('Failed to fetch activity:', err)
				setError('通知を読み込めなかった。')
				setLoading(false)
			})
		return () => controller.abort()
	}, [filter, activityVersion, reloadToken])

	return {
		items,
		firingCount,
		filter,
		setFilter: changeFilter,
		loading,
		error,
		reload
	}
}
