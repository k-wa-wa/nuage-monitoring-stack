import { useEffect, useRef, type RefObject } from 'react'

export type SwipeTargetLayer = 'layer1' | 'thread' | null

interface UseSwipeBackOptions {
	shellRef: RefObject<HTMLDivElement | null>
	targetLayer: SwipeTargetLayer
	enabled: boolean
	disabled: boolean
	onBack: () => void
}

const EDGE_THRESHOLD_PX = 36
const LOCK_DISTANCE_PX = 8
const COMPLETE_PROGRESS_THRESHOLD = 0.35
const COMPLETE_VELOCITY_THRESHOLD = 0.35

function hasScrollableHorizontalParent(target: EventTarget | null, stopAt: HTMLElement): boolean {
	let node = target instanceof HTMLElement ? target : null
	while (node && node !== stopAt) {
		if (node.scrollWidth > node.clientWidth + 1 && node.scrollLeft > 0) {
			const style = window.getComputedStyle(node)
			if (style.overflowX === 'auto' || style.overflowX === 'scroll') {
				return true
			}
		}
		node = node.parentElement
	}
	return false
}

/**
 * スマホ幅において画面左端からの右スワイプで 1 つ前の画面へ戻るフックである。
 * React の再レンダーを介さず CSS 変数 (--swipe-progress) を更新することで
 * 指に追従する滑らかなトランジションを実現する。
 * touchmove (passive: false) は左端スワイプ追跡中のみ動的に付与し、通常の縦スクロールを妨げない。
 */
export function useSwipeBack({
	shellRef,
	targetLayer,
	enabled,
	disabled,
	onBack
}: UseSwipeBackOptions): void {
	const onBackRef = useRef(onBack)
	const targetLayerRef = useRef(targetLayer)
	const disabledRef = useRef(disabled)

	useEffect(() => {
		onBackRef.current = onBack
	}, [onBack])

	useEffect(() => {
		targetLayerRef.current = targetLayer
	}, [targetLayer])

	useEffect(() => {
		disabledRef.current = disabled
	}, [disabled])

	useEffect(() => {
		if (!enabled) return
		const shell = shellRef.current
		if (!shell) return

		let tracking = false
		let locked = false
		let startX = 0
		let startY = 0
		let lastX = 0
		let lastTime = 0
		let velocityX = 0
		let activeLayer: SwipeTargetLayer = null
		let touchListenersAttached = false
		let pointerListenersAttached = false

		const clearSwipeStyles = () => {
			shell.classList.remove('is-swiping', 'is-swiping-layer1', 'is-swiping-thread')
			shell.style.removeProperty('--swipe-progress')
		}

		const canStartSwipe = (clientX: number, target: EventTarget | null): boolean => {
			if (disabledRef.current) return false
			if (!targetLayerRef.current) return false
			if (window.innerWidth >= 768) return false
			if (clientX > EDGE_THRESHOLD_PX) return false
			if (document.querySelector('.image-lightbox-backdrop')) return false
			if (hasScrollableHorizontalParent(target, shell)) return false
			return true
		}

		const detachTouchListeners = () => {
			if (!touchListenersAttached) return
			touchListenersAttached = false
			window.removeEventListener('touchmove', handleTouchMove)
			window.removeEventListener('touchend', handleTouchEnd)
			window.removeEventListener('touchcancel', handleTouchCancel)
		}

		const detachPointerListeners = () => {
			if (!pointerListenersAttached) return
			pointerListenersAttached = false
			window.removeEventListener('pointermove', handlePointerMove)
			window.removeEventListener('pointerup', handlePointerUp)
			window.removeEventListener('pointercancel', handlePointerCancel)
		}

		const abortTracking = () => {
			tracking = false
			locked = false
			activeLayer = null
			clearSwipeStyles()
			detachTouchListeners()
			detachPointerListeners()
		}

		const beginTrack = (clientX: number, clientY: number, target: EventTarget | null): boolean => {
			if (!canStartSwipe(clientX, target)) return false
			tracking = true
			locked = false
			activeLayer = targetLayerRef.current
			startX = clientX
			startY = clientY
			lastX = clientX
			lastTime = performance.now()
			velocityX = 0
			return true
		}

		const updateTrack = (clientX: number, clientY: number, preventDefault?: () => void) => {
			if (!tracking || !activeLayer) return

			const dx = clientX - startX
			const dy = clientY - startY

			if (!locked) {
				if (Math.abs(dx) < LOCK_DISTANCE_PX && Math.abs(dy) < LOCK_DISTANCE_PX) {
					return
				}
				if (dx <= 0 || Math.abs(dy) > Math.abs(dx) * 0.85) {
					abortTracking()
					return
				}
				locked = true
				shell.classList.add(
					'is-swiping',
					activeLayer === 'thread' ? 'is-swiping-thread' : 'is-swiping-layer1'
				)
			}

			preventDefault?.()

			const now = performance.now()
			const dt = now - lastTime
			if (dt > 0) {
				const instantVx = (clientX - lastX) / dt
				velocityX = velocityX * 0.4 + instantVx * 0.6
			}
			lastX = clientX
			lastTime = now

			const width = Math.max(shell.clientWidth, 1)
			const progress = Math.max(0, Math.min(1, dx / width))
			shell.style.setProperty('--swipe-progress', progress.toFixed(4))
		}

		const finishTrack = (clientX: number, cancelled = false) => {
			if (!tracking) {
				detachTouchListeners()
				detachPointerListeners()
				return
			}
			const wasLocked = locked
			const width = Math.max(shell.clientWidth, 1)
			const progress = Math.max(0, Math.min(1, (clientX - startX) / width))
			const shouldComplete =
				!cancelled &&
				wasLocked &&
				(progress >= COMPLETE_PROGRESS_THRESHOLD ||
					(velocityX >= COMPLETE_VELOCITY_THRESHOLD && progress >= 0.08))

			abortTracking()

			if (shouldComplete) {
				onBackRef.current()
			}
		}

		// Touch Events (左端で追跡を開始した時だけ touchmove passive: false を付与する)
		function handleTouchMove(e: TouchEvent) {
			if (!tracking || e.touches.length !== 1) return
			const t = e.touches[0]
			updateTrack(t.clientX, t.clientY, () => {
				if (e.cancelable) e.preventDefault()
			})
		}

		function handleTouchEnd(e: TouchEvent) {
			const t = e.changedTouches[0]
			finishTrack(t ? t.clientX : lastX, false)
		}

		function handleTouchCancel() {
			finishTrack(lastX, true)
		}

		const handleTouchStart = (e: TouchEvent) => {
			if (e.touches.length !== 1) {
				finishTrack(lastX, true)
				return
			}
			const t = e.touches[0]
			if (!beginTrack(t.clientX, t.clientY, e.target)) return
			if (!touchListenersAttached) {
				touchListenersAttached = true
				window.addEventListener('touchmove', handleTouchMove, { passive: false })
				window.addEventListener('touchend', handleTouchEnd, { passive: true })
				window.addEventListener('touchcancel', handleTouchCancel, { passive: true })
			}
		}

		// Pointer Events (マウスドラッグや Playwright pointer 操作のフォールバック)
		function handlePointerMove(e: PointerEvent) {
			if (e.pointerType === 'touch') return
			updateTrack(e.clientX, e.clientY)
		}

		function handlePointerUp(e: PointerEvent) {
			if (e.pointerType === 'touch') return
			finishTrack(e.clientX, false)
		}

		function handlePointerCancel(e: PointerEvent) {
			if (e.pointerType === 'touch') return
			finishTrack(lastX, true)
		}

		const handlePointerDown = (e: PointerEvent) => {
			if (e.pointerType === 'touch') return
			if (e.button !== 0) return
			if (!beginTrack(e.clientX, e.clientY, e.target)) return
			if (!pointerListenersAttached) {
				pointerListenersAttached = true
				window.addEventListener('pointermove', handlePointerMove)
				window.addEventListener('pointerup', handlePointerUp)
				window.addEventListener('pointercancel', handlePointerCancel)
			}
		}

		shell.addEventListener('touchstart', handleTouchStart, { passive: true })
		shell.addEventListener('pointerdown', handlePointerDown)

		return () => {
			abortTracking()
			shell.removeEventListener('touchstart', handleTouchStart)
			shell.removeEventListener('pointerdown', handlePointerDown)
		}
	}, [shellRef, enabled])
}
