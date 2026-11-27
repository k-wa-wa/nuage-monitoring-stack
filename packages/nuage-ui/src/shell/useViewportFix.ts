import { useEffect } from 'react'

function isEditableElement(el: Element | null): boolean {
	if (!el) return false
	const tag = el.tagName
	if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
	return (el as HTMLElement).isContentEditable === true
}

/**
 * iPhone (Safari のタブ・ホーム画面 PWA) のソフトウェアキーボード開閉に合わせて
 * --app-height / --app-offset-top を更新し、閉じた際のスクロールずれを戻すフックである。
 * あわせて通常画面でのピンチズーム (iOS gesturestart 等) を抑止する。
 */
export function useViewportFix(): void {
	useEffect(() => {
		const root = document.documentElement
		let maxObservedHeight = window.innerHeight
		let wasKeyboardOpen = false
		let resetTimer: number | null = null
		let multiTouchMoveAttached = false

		const syncViewport = () => {
			const vv = window.visualViewport
			const innerH = window.innerHeight
			if (innerH > maxObservedHeight) {
				maxObservedHeight = innerH
			}

			const visibleHeight = vv ? vv.height : innerH
			const offsetTop = vv ? Math.max(0, vv.offsetTop) : 0
			const baselineHeight = Math.max(innerH, maxObservedHeight)
			const keyboardOpen =
				baselineHeight - visibleHeight > 120 && isEditableElement(document.activeElement)

			if (keyboardOpen) {
				root.dataset.keyboardOpen = 'true'
				root.style.setProperty('--app-height', `${Math.round(visibleHeight)}px`)
				root.style.setProperty('--app-offset-top', `${Math.round(offsetTop)}px`)
			} else {
				// キーボードが閉じている間は CSS (#root の inset: 0) で画面全体を覆う。
				// iOS のホーム画面 PWA では、起動直後の visualViewport.height がセーフエリア分だけ短いことがあり、
				// その値で高さを固定すると下に隙間が残るため、ここでは高さを指定しない。
				delete root.dataset.keyboardOpen
				root.style.removeProperty('--app-height')
				root.style.setProperty('--app-offset-top', '0px')
				// キーボードが閉じた直後にレイアウトビューポートのスクロールずれを補正する
				if (wasKeyboardOpen || window.scrollY !== 0 || window.scrollX !== 0 || offsetTop > 0) {
					window.scrollTo(0, 0)
				}
			}
			wasKeyboardOpen = keyboardOpen
		}

		const handleFocusIn = () => {
			if (resetTimer !== null) {
				window.clearTimeout(resetTimer)
				resetTimer = null
			}
			// iOS でキーボードアニメーション後に値が確定するため少し遅らせても同期する
			window.setTimeout(syncViewport, 60)
			window.setTimeout(syncViewport, 260)
		}

		const handleFocusOut = () => {
			if (resetTimer !== null) {
				window.clearTimeout(resetTimer)
			}
			resetTimer = window.setTimeout(() => {
				resetTimer = null
				if (!isEditableElement(document.activeElement)) {
					window.scrollTo(0, 0)
					syncViewport()
				}
			}, 120)
		}

		const handleOrientationOrResize = () => {
			maxObservedHeight = Math.max(maxObservedHeight, window.innerHeight)
			syncViewport()
		}

		// iOS Safari は user-scalable=no を無視してピンチ拡大を許すため gesture* イベントを抑止する
		const preventGestureZoom = (e: Event) => {
			if (e.cancelable) {
				e.preventDefault()
			}
		}

		const preventMultiTouchMove = (e: TouchEvent) => {
			if (e.touches.length >= 2 && e.cancelable) {
				e.preventDefault()
			}
		}

		const detachMultiTouchMove = () => {
			if (!multiTouchMoveAttached) return
			multiTouchMoveAttached = false
			window.removeEventListener('touchmove', preventMultiTouchMove)
			window.removeEventListener('touchend', handleMultiTouchEnd)
			window.removeEventListener('touchcancel', handleMultiTouchEnd)
		}

		function handleMultiTouchEnd(e: TouchEvent) {
			if (e.touches.length < 2) {
				detachMultiTouchMove()
			}
		}

		// 1 本指の通常スクロールを妨げないよう、2 本指以上のタッチ開始時のみ touchmove (passive: false) を付ける
		const handlePinchTouchStart = (e: TouchEvent) => {
			if (e.touches.length >= 2 && !multiTouchMoveAttached) {
				multiTouchMoveAttached = true
				window.addEventListener('touchmove', preventMultiTouchMove, { passive: false })
				window.addEventListener('touchend', handleMultiTouchEnd, { passive: true })
				window.addEventListener('touchcancel', handleMultiTouchEnd, { passive: true })
			}
		}

		syncViewport()

		const vv = window.visualViewport
		if (vv) {
			vv.addEventListener('resize', syncViewport)
			vv.addEventListener('scroll', syncViewport)
		}
		window.addEventListener('resize', handleOrientationOrResize)
		document.addEventListener('focusin', handleFocusIn)
		document.addEventListener('focusout', handleFocusOut)
		document.addEventListener('gesturestart', preventGestureZoom, { passive: false })
		document.addEventListener('gesturechange', preventGestureZoom, { passive: false })
		document.addEventListener('touchstart', handlePinchTouchStart, { passive: true })

		return () => {
			if (resetTimer !== null) {
				window.clearTimeout(resetTimer)
			}
			detachMultiTouchMove()
			if (vv) {
				vv.removeEventListener('resize', syncViewport)
				vv.removeEventListener('scroll', syncViewport)
			}
			window.removeEventListener('resize', handleOrientationOrResize)
			document.removeEventListener('focusin', handleFocusIn)
			document.removeEventListener('focusout', handleFocusOut)
			document.removeEventListener('gesturestart', preventGestureZoom)
			document.removeEventListener('gesturechange', preventGestureZoom)
			document.removeEventListener('touchstart', handlePinchTouchStart)
		}
	}, [])
}
