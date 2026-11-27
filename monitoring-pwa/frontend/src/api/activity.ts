import type { ActivityFilter, ActivityResponse } from '../types/chat'
import { ApiError, apiFetch } from './client'

/** 通知タブの一覧を取得する。 */
export async function fetchActivity(
	filter: ActivityFilter,
	limit = 100,
	signal?: AbortSignal
): Promise<ActivityResponse> {
	const res = await apiFetch(`/api/activity?filter=${encodeURIComponent(filter)}&limit=${limit}`, { signal })
	if (!res) throw new ApiError(401, '認証が切れた。')
	if (!res.ok) throw new ApiError(res.status)
	return (await res.json()) as ActivityResponse
}

/** 発生中のアラート件数だけを取得する。取得できない場合は null を返す。 */
export async function fetchFiringCount(signal?: AbortSignal): Promise<number | null> {
	const res = await apiFetch('/api/activity?filter=firing&limit=1', { signal })
	if (!res || !res.ok) return null
	const data = (await res.json()) as ActivityResponse
	return data.firing_count
}
