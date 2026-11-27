import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'

const BOTTOM_THRESHOLD_PX = 80
const LOAD_OLDER_THRESHOLD_PX = 160

interface Options {
	scrollRef: RefObject<HTMLDivElement | null>
	// 表示中のメッセージの ts の並び (古い順)
	itemIds: string[]
	hasMore: boolean
	loadingOlder: boolean
	onLoadOlder: () => void
}

/**
 * チャットらしいスクロールを行う。
 * - 初めて中身が出たら一番下を見せる
 * - 古い履歴を先頭に足しても、見ていた位置を保つ
 * - 一番下付近を見ている時だけ新着を追いかけ、上を読んでいる時は「新着あり」を立てる
 * - キーボードの開閉や画像の読み込みで高さが変わっても、一番下にいれば一番下に留まる
 */
export function useChatScroll({ scrollRef, itemIds, hasMore, loadingOlder, onLoadOlder }: Options) {
	const atBottomRef = useRef(true)
	const prevIdsRef = useRef<string[]>([])
	const prevScrollHeightRef = useRef(0)
	const [hasNewMessages, setHasNewMessages] = useState(false)
	const loadOlderRef = useRef({ hasMore, loadingOlder, onLoadOlder })
	useLayoutEffect(() => {
		loadOlderRef.current = { hasMore, loadingOlder, onLoadOlder }
	})

	const scrollToBottom = useCallback(
		(smooth = false) => {
			const el = scrollRef.current
			if (!el) return
			el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
			atBottomRef.current = true
			setHasNewMessages(false)
		},
		[scrollRef]
	)

	const handleScroll = useCallback(() => {
		const el = scrollRef.current
		if (!el) return
		atBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD_PX
		if (atBottomRef.current) setHasNewMessages(false)
		const { hasMore: more, loadingOlder: busy, onLoadOlder: load } = loadOlderRef.current
		if (el.scrollTop < LOAD_OLDER_THRESHOLD_PX && more && !busy) load()
	}, [scrollRef])

	// 中身の変化に合わせてスクロール位置を決める (描画直後・画面に出る前に行う)
	useLayoutEffect(() => {
		const el = scrollRef.current
		if (!el) return
		const prev = prevIdsRef.current
		const next = itemIds
		if (prev.length === 0 && next.length > 0) {
			el.scrollTop = el.scrollHeight
			atBottomRef.current = true
		} else if (next.length > prev.length && prev.length > 0) {
			const prepended = next[0] !== prev[0] && next[next.length - 1] === prev[prev.length - 1]
			if (prepended) {
				el.scrollTop += el.scrollHeight - prevScrollHeightRef.current
			} else if (atBottomRef.current) {
				el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
			} else {
				setHasNewMessages(true)
			}
		}
		prevIdsRef.current = next
		prevScrollHeightRef.current = el.scrollHeight
	}, [itemIds, scrollRef])

	// キーボードの開閉・画像の読み込みで高さが変わっても、一番下にいれば留まる
	useEffect(() => {
		const el = scrollRef.current
		if (!el || typeof ResizeObserver === 'undefined') return
		const observer = new ResizeObserver(() => {
			if (atBottomRef.current) el.scrollTop = el.scrollHeight
			prevScrollHeightRef.current = el.scrollHeight
		})
		observer.observe(el)
		const inner = el.querySelector(':scope .chat-inner')
		if (inner) observer.observe(inner)
		return () => observer.disconnect()
	}, [scrollRef])

	// 自分が送信した時は、上を読んでいても一番下へ移る
	const stickToBottomOnNext = useCallback(() => {
		atBottomRef.current = true
	}, [])

	return { handleScroll, hasNewMessages, scrollToBottom, stickToBottomOnNext }
}
