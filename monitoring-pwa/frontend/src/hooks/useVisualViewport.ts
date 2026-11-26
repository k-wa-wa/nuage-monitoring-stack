import { useEffect } from 'react'

function isEditableElement(el: Element | null): boolean {
	if (!el) return false
	const tag = el.tagName
	if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
	return (el as HTMLElement).isContentEditable === true
}

/**
 * iOS Safari / PWA のソフトウェアキーボード開閉に合わせて
 * --app-height / --app-offset-top を更新し、閉じた際のスクロールずれを戻すフックである。
 */
export function useVisualViewport(): void {
	useEffect(() => {
		const root = document.documentElement
		let maxObservedHeight = window.innerHeight
		let wasKeyboardOpen = false
		let resetTimer: number | null = null

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

			root.style.setProperty('--app-height', `${Math.round(visibleHeight)}px`)

			if (keyboardOpen) {
				root.dataset.keyboardOpen = 'true'
				root.style.setProperty('--app-offset-top', `${Math.round(offsetTop)}px`)
			} else {
				delete root.dataset.keyboardOpen
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

		syncViewport()

		const vv = window.visualViewport
		if (vv) {
			vv.addEventListener('resize', syncViewport)
			vv.addEventListener('scroll', syncViewport)
		}
		window.addEventListener('resize', handleOrientationOrResize)
		document.addEventListener('focusin', handleFocusIn)
		document.addEventListener('focusout', handleFocusOut)

		return () => {
			if (resetTimer !== null) {
				window.clearTimeout(resetTimer)
			}
			if (vv) {
				vv.removeEventListener('resize', syncViewport)
				vv.removeEventListener('scroll', syncViewport)
			}
			window.removeEventListener('resize', handleOrientationOrResize)
			document.removeEventListener('focusin', handleFocusIn)
			document.removeEventListener('focusout', handleFocusOut)
		}
	}, [])
}
