import { useCallback, useEffect, useState } from 'react'
import { searchMessages } from '../api/chat'
import type { SearchResult } from '../types/chat'

const SEARCH_DEBOUNCE_MS = 250

/** チャンネル・メッセージのキーワード検索状態 (デバウンス中の検索中表示を含む) を管理する。 */
export function useChannelSearch() {
	const [query, setQuery] = useState('')
	const [results, setResults] = useState<SearchResult[] | null>(null)
	const [searching, setSearching] = useState(false)
	const [error, setError] = useState<string | null>(null)

	const handleQueryChange = useCallback((next: string) => {
		setQuery(next)
		if (!next.trim()) {
			setResults(null)
			setSearching(false)
			setError(null)
		} else {
			setSearching(true)
			setError(null)
		}
	}, [])

	useEffect(() => {
		const q = query.trim()
		if (!q) return
		const controller = new AbortController()
		const timer = window.setTimeout(async () => {
			try {
				const data = await searchMessages(q, controller.signal)
				if (!controller.signal.aborted) {
					setResults(data)
					setError(null)
				}
			} catch (err) {
				if ((err as Error).name !== 'AbortError') {
					setError('検索できなかった。')
				}
			} finally {
				if (!controller.signal.aborted) {
					setSearching(false)
				}
			}
		}, SEARCH_DEBOUNCE_MS)
		return () => {
			controller.abort()
			window.clearTimeout(timer)
		}
	}, [query])

	return {
		query,
		setQuery: handleQueryChange,
		results,
		searching,
		error
	}
}
