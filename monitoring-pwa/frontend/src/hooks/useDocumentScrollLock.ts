import { useEffect } from 'react'

type Axis = 'x' | 'y'

function canScroll(el: HTMLElement, axis: Axis, delta: number): boolean {
	const style = window.getComputedStyle(el)
	const overflow = axis === 'y' ? style.overflowY : style.overflowX
	if (overflow !== 'auto' && overflow !== 'scroll') return false

	if (axis === 'y') {
		if (el.scrollHeight <= el.clientHeight + 1) return false
		// 指が下へ動く (delta > 0) と上へスクロールする
		if (delta > 0) return el.scrollTop > 0
		if (delta < 0) return el.scrollTop + el.clientHeight < el.scrollHeight - 1
		return true
	}
	if (el.scrollWidth <= el.clientWidth + 1) return false
	if (delta > 0) return el.scrollLeft > 0
	if (delta < 0) return el.scrollLeft + el.clientWidth < el.scrollWidth - 1
	return true
}

function hasScrollableAncestor(target: EventTarget | null, axis: Axis, delta: number): boolean {
	let node: Element | null = target instanceof Element ? target : null
	while (node && node !== document.body && node !== document.documentElement) {
		if (node instanceof HTMLElement) {
			// 1 行の入力欄はカーソル移動や文字選択のドラッグを妨げない
			if (node.tagName === 'INPUT') return true
			if (canScroll(node, axis, delta)) return true
		}
		node = node.parentElement
	}
	return false
}

/**
 * ブラウザ (特に iOS Safari のタブ表示) で、文書全体がスクロール・ゴム跳ねしないようにするフックである。
 *
 * html/body の overflow: hidden と overscroll-behavior だけでは、iOS Safari は
 * スクロールできない領域 (ヘッダー・中身の少ない一覧など) へのタッチを文書全体のスクロールに回してしまう。
 * そこで 1 本指の touchmove ごとに、その向きへスクロールできる祖先要素があるかを調べ、
 * 無ければ preventDefault して文書へ伝わらないようにする。
 * スクロールできる領域の端での跳ねは、各領域の overscroll-behavior: contain に任せる。
 */
export function useDocumentScrollLock(): void {
	useEffect(() => {
		let startX = 0
		let startY = 0
		let tracking = false

		const handleTouchStart = (e: TouchEvent) => {
			if (e.touches.length !== 1) {
				tracking = false
				return
			}
			tracking = true
			startX = e.touches[0].clientX
			startY = e.touches[0].clientY
		}

		const handleTouchMove = (e: TouchEvent) => {
			if (!tracking || e.touches.length !== 1 || !e.cancelable) return
			const dx = e.touches[0].clientX - startX
			const dy = e.touches[0].clientY - startY
			if (dx === 0 && dy === 0) return
			const axis: Axis = Math.abs(dy) >= Math.abs(dx) ? 'y' : 'x'
			const delta = axis === 'y' ? dy : dx
			if (!hasScrollableAncestor(e.target, axis, delta)) {
				e.preventDefault()
			}
		}

		const handleTouchEnd = () => {
			tracking = false
		}

		document.addEventListener('touchstart', handleTouchStart, { passive: true })
		document.addEventListener('touchmove', handleTouchMove, { passive: false })
		document.addEventListener('touchend', handleTouchEnd, { passive: true })
		document.addEventListener('touchcancel', handleTouchEnd, { passive: true })

		// キーボードを閉じた後などに文書が少しでもスクロールしていたら戻す。
		// キーボード表示中は iOS が入力欄を見せるために動かすので触らない (useVisualViewport が追従する)。
		const handleWindowScroll = () => {
			if (document.documentElement.dataset.keyboardOpen === 'true') return
			const active = document.activeElement
			if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA')) return
			if (window.scrollX !== 0 || window.scrollY !== 0) {
				window.scrollTo(0, 0)
			}
		}
		window.addEventListener('scroll', handleWindowScroll, { passive: true })

		return () => {
			document.removeEventListener('touchstart', handleTouchStart)
			document.removeEventListener('touchmove', handleTouchMove)
			document.removeEventListener('touchend', handleTouchEnd)
			document.removeEventListener('touchcancel', handleTouchEnd)
			window.removeEventListener('scroll', handleWindowScroll)
		}
	}, [])
}
