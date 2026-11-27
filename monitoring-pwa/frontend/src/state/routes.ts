import { usePageLocation } from '@nuage/ui'

// チャンネル・スレッドは各タブの中に積む (タブごとに履歴を持つ)。
// PC・タブレットの横並びでも同じ URL を使う。
export type TabBase = '/tabs/home' | '/tabs/activity'

/** この画面が属するタブ (ホームから開いたか、通知から開いたか) */
export function useTabBase(): TabBase {
	const { pathname } = usePageLocation()
	return pathname.startsWith('/tabs/activity') ? '/tabs/activity' : '/tabs/home'
}

export const channelPath = (base: TabBase, channelId: string) =>
	`${base}/channels/${encodeURIComponent(channelId)}`

export const threadPath = (base: TabBase, channelId: string, ts: string) =>
	`${channelPath(base, channelId)}/threads/${encodeURIComponent(ts)}`

/** 直接開いた URL から戻る先 (スレッド → チャンネル → タブのいちばん下) */
export function parentPath(pathname: string): string | null {
	const thread = pathname.match(/^(\/tabs\/(?:home|activity))\/channels\/([^/]+)\/threads\/[^/]+$/)
	if (thread) return thread[1] === '/tabs/activity' ? '/tabs/activity' : `${thread[1]}/channels/${thread[2]}`
	const channel = pathname.match(/^(\/tabs\/(?:home|activity))\/channels\/[^/]+$/)
	if (channel) return channel[1]
	return null
}
