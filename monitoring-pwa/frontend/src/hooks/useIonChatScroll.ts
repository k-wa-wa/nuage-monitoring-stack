import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'

const BOTTOM_THRESHOLD_PX = 80
const LOAD_OLDER_THRESHOLD_PX = 160

interface Options {
	contentRef: RefObject<HTMLIonContentElement | null>
	// 表示中のメッセージの ts の並び (古い順)
	itemIds: string[]
	hasMore: boolean
	loadingOlder: boolean
	onLoadOlder: () => void
}

/**
 * IonContent でチャットらしいスクロールを行うフックである。
 * - 初めて中身が出たら一番下を見せる
 * - 古い履歴を先頭に足しても、見ていた位置を保つ
 * - 一番下付近を見ている時だけ新着を追いかけ、上を読んでいる時は「新着あり」を立てる
 * - キーボードの開閉や画像の読み込みで高さが変わっても、一番下にいれば一番下に留まる
 */
export function useIonChatScroll({ contentRef, itemIds, hasMore, loadingOlder, onLoadOlder }: Options) {
	const scrollElRef = useRef<HTMLElement | null>(null)
	const [scrollReady, setScrollReady] = useState(false)
	const atBottomRef = useRef(true)
	const prevIdsRef = useRef<string[]>([])
	const prevScrollHeightRef = useRef(0)
	const [hasNewMessages, setHasNewMessages] = useState(false)
	const loadOlderRef = useRef({ hasMore, loadingOlder, onLoadOlder })
	loadOlderRef.current = { hasMore, loadingOlder, onLoadOlder }

	useEffect(() => {
		let cancelled = false
		const content = contentRef.current
		if (!content) return
		void content.getScrollElement().then((el) => {
			if (cancelled) return
			scrollElRef.current = el
			setScrollReady(true)
		})
		return () => {
			cancelled = true
		}
	}, [contentRef])

	const scrollToBottom = useCallback((smooth = false) => {
		const el = scrollElRef.current
		if (!el) return
		el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
		atBottomRef.current = true
		setHasNewMessages(false)
	}, [])

	const handleScroll = useCallback(() => {
		const el = scrollElRef.current
		if (!el) return
		const distance = el.scrollHeight - el.scrollTop - el.clientHeight
		atBottomRef.current = distance < BOTTOM_THRESHOLD_PX
		if (atBottomRef.current) setHasNewMessages(false)
		const { hasMore: more, loadingOlder: busy, onLoadOlder: load } = loadOlderRef.current
		if (el.scrollTop < LOAD_OLDER_THRESHOLD_PX && more && !busy) load()
	}, [])

	// 中身の変化に合わせてスクロール位置を決める (描画直後・画面に出る前に行う)
	useLayoutEffect(() => {
		const el = scrollElRef.current
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
	}, [itemIds, scrollReady])

	// キーボードの開閉・画像の読み込み・画面の回転で高さが変わっても、一番下にいれば留まる
	useEffect(() => {
		const el = scrollElRef.current
		if (!el || typeof ResizeObserver === 'undefined') return
		const observer = new ResizeObserver(() => {
			if (atBottomRef.current) el.scrollTop = el.scrollHeight
			prevScrollHeightRef.current = el.scrollHeight
		})
		observer.observe(el)
		// 中身は IonContent の子 (.chat-inner) に描かれる。読み込み中の表示から本文に差し替わっても
		// 見張り続けられるよう、差し替わらない入れ物を見張る
		const inner = contentRef.current?.querySelector(':scope > .chat-inner')
		if (inner) observer.observe(inner)
		return () => observer.disconnect()
	}, [scrollReady, contentRef])

	// 自分が送信した時は、上を読んでいても一番下へ移る
	const stickToBottomOnNext = useCallback(() => {
		atBottomRef.current = true
	}, [])

	return { handleScroll, hasNewMessages, scrollToBottom, stickToBottomOnNext }
}
