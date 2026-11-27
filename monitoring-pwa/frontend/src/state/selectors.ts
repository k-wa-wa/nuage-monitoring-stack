import { useMemo } from 'react'
import { useChatState } from './chatStore'

// チャンネル ID から名前を引く表 (本文中の <#C…> の表示に使う)
export function useChannelsById(): Record<string, string> {
	const channels = useChatState((s) => s.channels)
	return useMemo(() => {
		const map: Record<string, string> = {}
		for (const ch of channels) map[ch.id] = ch.name
		return map
	}, [channels])
}
