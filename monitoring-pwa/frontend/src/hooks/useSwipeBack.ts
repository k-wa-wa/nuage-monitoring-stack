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

		const beginTrack = (clientX: number, clientY: number, target: EventTarget | null) => {
			if (!canStartSwipe(clientX, target)) return
			tracking = true
			locked = false
			activeLayer = targetLayerRef.current
			startX = clientX
			startY = clientY
			lastX = clientX
			lastTime = performance.now()
			velocityX = 0
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
					tracking = false
					activeLayer = null
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
			if (!tracking) return
			const wasLocked = locked
			const width = Math.max(shell.clientWidth, 1)
			const progress = Math.max(0, Math.min(1, (clientX - startX) / width))
			const shouldComplete =
				!cancelled &&
				wasLocked &&
				(progress >= COMPLETE_PROGRESS_THRESHOLD ||
					(velocityX >= COMPLETE_VELOCITY_THRESHOLD && progress >= 0.08))

			tracking = false
			locked = false
			activeLayer = null
			clearSwipeStyles()

			if (shouldComplete) {
				onBackRef.current()
			}
		}

		// Touch Events (iOS Safari / Android Chrome の実タッチ操作用。passive: false で縦スクロールを抑止する)
		const handleTouchStart = (e: TouchEvent) => {
			if (e.touches.length !== 1) {
				finishTrack(lastX, true)
				return
			}
			const t = e.touches[0]
			beginTrack(t.clientX, t.clientY, e.target)
		}

		const handleTouchMove = (e: TouchEvent) => {
			if (!tracking || e.touches.length !== 1) return
			const t = e.touches[0]
			updateTrack(t.clientX, t.clientY, () => {
				if (e.cancelable) e.preventDefault()
			})
		}

		const handleTouchEnd = (e: TouchEvent) => {
			const t = e.changedTouches[0]
			finishTrack(t ? t.clientX : lastX, false)
		}

		const handleTouchCancel = () => {
			finishTrack(lastX, true)
		}

		// Pointer Events (マウスドラッグや Playwright pointer 操作のフォールバック)
		const handlePointerDown = (e: PointerEvent) => {
			if (e.pointerType === 'touch') return
			if (e.button !== 0) return
			beginTrack(e.clientX, e.clientY, e.target)
		}

		const handlePointerMove = (e: PointerEvent) => {
			if (e.pointerType === 'touch') return
			updateTrack(e.clientX, e.clientY)
		}

		const handlePointerUp = (e: PointerEvent) => {
			if (e.pointerType === 'touch') return
			finishTrack(e.clientX, false)
		}

		const handlePointerCancel = (e: PointerEvent) => {
			if (e.pointerType === 'touch') return
			finishTrack(lastX, true)
		}

		shell.addEventListener('touchstart', handleTouchStart, { passive: true })
		shell.addEventListener('touchmove', handleTouchMove, { passive: false })
		shell.addEventListener('touchend', handleTouchEnd, { passive: true })
		shell.addEventListener('touchcancel', handleTouchCancel, { passive: true })
		shell.addEventListener('pointerdown', handlePointerDown)
		window.addEventListener('pointermove', handlePointerMove)
		window.addEventListener('pointerup', handlePointerUp)
		window.addEventListener('pointercancel', handlePointerCancel)

		return () => {
			clearSwipeStyles()
			shell.removeEventListener('touchstart', handleTouchStart)
			shell.removeEventListener('touchmove', handleTouchMove)
			shell.removeEventListener('touchend', handleTouchEnd)
			shell.removeEventListener('touchcancel', handleTouchCancel)
			shell.removeEventListener('pointerdown', handlePointerDown)
			window.removeEventListener('pointermove', handlePointerMove)
			window.removeEventListener('pointerup', handlePointerUp)
			window.removeEventListener('pointercancel', handlePointerCancel)
		}
	}, [shellRef, enabled])
}
