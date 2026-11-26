import { useLocation } from 'react-router-dom'

// チャンネル・スレッドは各タブの中に積む (Ionic の作法。タブごとに履歴を持つ)。
// タブの外に置くと、開いている間にタブの中の画面が外れ、戻った時に中身の無いタブ (真っ暗) になることがあった。
export type TabBase = '/tabs/home' | '/tabs/activity'

export function useTabBase(): TabBase {
	const { pathname } = useLocation()
	return pathname.startsWith('/tabs/activity') ? '/tabs/activity' : '/tabs/home'
}

export const channelPath = (base: TabBase, channelId: string) =>
	`${base}/channels/${encodeURIComponent(channelId)}`

export const threadPath = (base: TabBase, channelId: string, ts: string) =>
	`${channelPath(base, channelId)}/threads/${encodeURIComponent(ts)}`
