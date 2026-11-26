import { useCallback, useEffect, useLayoutEffect, useRef, useState, type UIEvent } from 'react'

interface UseChatScrollOptions {
	scrollKey: string
	itemIds: string[]
	isInitialLoading?: boolean
	hasMore?: boolean
	loadingOlder?: boolean
	onLoadOlder?: () => Promise<void>
}

interface ScrollAnchor {
	ts: string
	offsetTop: number
	scrollHeight: number
}

const BOTTOM_THRESHOLD_PX = 80
const LOAD_OLDER_THRESHOLD_PX = 60

function isNearBottom(el: HTMLElement): boolean {
	return el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD_PX
}

/**
 * チャットのタイムライン / スレッド向けに以下のスクロール挙動を提供するフックである。
 * - 初回および対象切替時は最下部へスクロールする
 * - 古い履歴を先頭に追加した際は見ていた要素の表示位置を維持する
 * - 一番下付近を見ている時だけ新着を追従し、上を読んでいる時は「新着あり」通知を出す
 * - キーボード開閉などでコンテナ高さが変化した際も最下部張り付きを維持する
 */
export function useChatScroll({
	scrollKey,
	itemIds,
	isInitialLoading = false,
	hasMore = false,
	loadingOlder = false,
	onLoadOlder
}: UseChatScrollOptions) {
	const scrollRef = useRef<HTMLDivElement | null>(null)
	const [hasNewMessages, setHasNewMessages] = useState(false)

	const prevKeyRef = useRef<string>(scrollKey)
	const needsInitialScrollRef = useRef<boolean>(true)
	const shouldStickBottomRef = useRef<boolean>(true)
	const forceBottomOnNextAppendRef = useRef<boolean>(false)
	const prevFirstIdRef = useRef<string | null>(null)
	const prevLastIdRef = useRef<string | null>(null)
	const prevCountRef = useRef<number>(0)
	const anchorRef = useRef<ScrollAnchor | null>(null)
	const loadingOlderRef = useRef<boolean>(loadingOlder)
	const hasMoreRef = useRef<boolean>(hasMore)
	const onLoadOlderRef = useRef(onLoadOlder)
	const prevScrollHeightRef = useRef<number>(0)
	const prevClientHeightRef = useRef<number>(0)

	const syncMetrics = useCallback((el: HTMLElement) => {
		prevScrollHeightRef.current = el.scrollHeight
		prevClientHeightRef.current = el.clientHeight
	}, [])

	useEffect(() => {
		loadingOlderRef.current = loadingOlder
	}, [loadingOlder])

	useEffect(() => {
		hasMoreRef.current = hasMore
	}, [hasMore])

	useEffect(() => {
		onLoadOlderRef.current = onLoadOlder
	}, [onLoadOlder])

	const prepareLoadOlder = useCallback(() => {
		const el = scrollRef.current
		if (!el) return
		const msgEls = el.querySelectorAll<HTMLElement>('[data-msg-ts]')
		const containerTop = el.getBoundingClientRect().top
		let anchorEl: HTMLElement | null = msgEls[0] || null
		for (const node of msgEls) {
			const rect = node.getBoundingClientRect()
			if (rect.bottom > containerTop) {
				anchorEl = node
				break
			}
		}
		const ts = anchorEl?.dataset.msgTs || prevFirstIdRef.current || ''
		const offsetTop = anchorEl ? anchorEl.getBoundingClientRect().top - containerTop : 0
		anchorRef.current = {
			ts,
			offsetTop,
			scrollHeight: el.scrollHeight
		}
	}, [])

	// scrollKey が変わったら初回スクロール状態へリセットする
	if (prevKeyRef.current !== scrollKey) {
		prevKeyRef.current = scrollKey
		needsInitialScrollRef.current = true
		shouldStickBottomRef.current = true
		forceBottomOnNextAppendRef.current = false
		prevFirstIdRef.current = null
		prevLastIdRef.current = null
		prevCountRef.current = 0
		anchorRef.current = null
		prevScrollHeightRef.current = 0
		prevClientHeightRef.current = 0
	}

	useEffect(() => {
		setHasNewMessages(false)
	}, [scrollKey])

	useLayoutEffect(() => {
		const el = scrollRef.current
		if (!el) return

		const firstId = itemIds[0] || null
		const lastId = itemIds[itemIds.length - 1] || null
		const count = itemIds.length

		if (needsInitialScrollRef.current) {
			if (count > 0 && !isInitialLoading) {
				el.scrollTop = el.scrollHeight
				shouldStickBottomRef.current = true
				needsInitialScrollRef.current = false
				setHasNewMessages(false)
			} else if (!isInitialLoading && count === 0) {
				el.scrollTop = el.scrollHeight
			}
		} else {
			const prependedOlder =
				Boolean(prevFirstIdRef.current && firstId && firstId !== prevFirstIdRef.current && count > prevCountRef.current)
			const appendedNewer = Boolean(lastId && lastId !== prevLastIdRef.current)

			if (prependedOlder) {
				// 上方向へ古い履歴が追加された場合はアンカー要素の表示位置を保つ
				const anchor = anchorRef.current
				let restored = false
				if (anchor?.ts) {
					const target = el.querySelector<HTMLElement>(`[data-msg-ts="${CSS.escape(anchor.ts)}"]`)
					if (target) {
						const containerTop = el.getBoundingClientRect().top
						const currentOffset = target.getBoundingClientRect().top - containerTop
						el.scrollTop += currentOffset - anchor.offsetTop
						restored = true
					}
				}
				if (!restored && anchor && anchor.scrollHeight > 0) {
					el.scrollTop += el.scrollHeight - anchor.scrollHeight
				}
				anchorRef.current = null
				shouldStickBottomRef.current = isNearBottom(el)
			}

			if (appendedNewer) {
				if (shouldStickBottomRef.current || forceBottomOnNextAppendRef.current) {
					el.scrollTop = el.scrollHeight
					shouldStickBottomRef.current = true
					forceBottomOnNextAppendRef.current = false
					setHasNewMessages(false)
				} else {
					setHasNewMessages(true)
				}
			}
		}

		prevFirstIdRef.current = firstId
		prevLastIdRef.current = lastId
		prevCountRef.current = count
		syncMetrics(el)
	}, [itemIds, scrollKey, isInitialLoading, syncMetrics])

	// キーボード表示や画面サイズ変化、画像遅延読込・詳細展開で高さが変わった時も最下部表示を維持する
	useEffect(() => {
		const el = scrollRef.current
		if (!el) return
		syncMetrics(el)

		const stickIfNeeded = () => {
			// ユーザーが上へスクロールした直後 (scroll イベント発火前) の高さ変化で引き戻さないよう、
			// 変化前のサイズ基準で最下部付近にいたかを確認する
			const prevSH = prevScrollHeightRef.current || el.scrollHeight
			const prevCH = prevClientHeightRef.current || el.clientHeight
			const wasNearBottom = prevSH - el.scrollTop - prevCH < BOTTOM_THRESHOLD_PX
			if (shouldStickBottomRef.current && wasNearBottom) {
				el.scrollTop = el.scrollHeight
			} else if (!wasNearBottom) {
				shouldStickBottomRef.current = false
			}
			syncMetrics(el)
		}

		let resizeObserver: ResizeObserver | null = null
		let mutationObserver: MutationObserver | null = null

		if (typeof ResizeObserver !== 'undefined') {
			resizeObserver = new ResizeObserver(stickIfNeeded)
			resizeObserver.observe(el)
			for (const child of Array.from(el.children)) {
				resizeObserver.observe(child)
			}

			if (typeof MutationObserver !== 'undefined') {
				mutationObserver = new MutationObserver((mutations) => {
					for (const mutation of mutations) {
						for (const added of Array.from(mutation.addedNodes)) {
							if (added instanceof HTMLElement) {
								resizeObserver?.observe(added)
							}
						}
						for (const removed of Array.from(mutation.removedNodes)) {
							if (removed instanceof HTMLElement) {
								resizeObserver?.unobserve(removed)
							}
						}
					}
					stickIfNeeded()
				})
				mutationObserver.observe(el, { childList: true })
			}
		}

		// img の load イベントはバブリングしないためキャプチャフェーズで拾う
		const handleMediaLoad = (event: Event) => {
			if (event.target instanceof HTMLImageElement) {
				stickIfNeeded()
			}
		}
		el.addEventListener('load', handleMediaLoad, true)

		return () => {
			el.removeEventListener('load', handleMediaLoad, true)
			mutationObserver?.disconnect()
			resizeObserver?.disconnect()
		}
	}, [scrollKey, syncMetrics])

	const scrollToBottom = useCallback(() => {
		const el = scrollRef.current
		if (!el) return
		el.scrollTop = el.scrollHeight
		shouldStickBottomRef.current = true
		forceBottomOnNextAppendRef.current = false
		syncMetrics(el)
		setHasNewMessages(false)
	}, [syncMetrics])

	const markStickBottomOnSend = useCallback(() => {
		shouldStickBottomRef.current = true
		forceBottomOnNextAppendRef.current = true
		setHasNewMessages(false)
	}, [])

	const handleScroll = useCallback(
		(e: UIEvent<HTMLDivElement>) => {
			const el = e.currentTarget
			syncMetrics(el)
			const nearBottom = isNearBottom(el)
			shouldStickBottomRef.current = nearBottom
			if (nearBottom) {
				setHasNewMessages(false)
			}

			if (
				el.scrollTop < LOAD_OLDER_THRESHOLD_PX &&
				hasMoreRef.current &&
				!loadingOlderRef.current &&
				onLoadOlderRef.current
			) {
				prepareLoadOlder()
				void onLoadOlderRef.current()
			}
		},
		[prepareLoadOlder, syncMetrics]
	)

	return {
		scrollRef,
		hasNewMessages,
		scrollToBottom,
		markStickBottomOnSend,
		handleScroll,
		prepareLoadOlder
	}
}
