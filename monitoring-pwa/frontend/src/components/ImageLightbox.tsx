import { useEffect, useRef, useState, type TouchEvent as ReactTouchEvent } from 'react'
import { Minimize2, Maximize2, X } from 'lucide-react'

interface ImageLightboxProps {
	src: string
	alt: string
	onClose: () => void
}

const MIN_SCALE = 1
const MAX_SCALE = 4
const ZOOM_TOGGLE_SCALE = 2

function getTouchDistance(touches: ReactTouchEvent<HTMLDivElement>['touches']): number {
	const dx = touches[0].clientX - touches[1].clientX
	const dy = touches[0].clientY - touches[1].clientY
	return Math.hypot(dx, dy)
}

/**
 * スマホ等でピンチ拡大を無効化した環境でも画像の詳細を読めるようにする全画面プレビューである。
 * 1x/2x タップ拡大およびアプリ内 2 本指ピンチ拡大・パンに対応する。
 */
export default function ImageLightbox({ src, alt, onClose }: ImageLightboxProps) {
	const [scale, setScale] = useState(MIN_SCALE)
	const [baseSize, setBaseSize] = useState<{ width: number; height: number } | null>(null)
	const imgRef = useRef<HTMLImageElement | null>(null)
	const pinchStartDistRef = useRef<number | null>(null)
	const pinchStartScaleRef = useRef<number>(MIN_SCALE)
	const pinchedDuringGestureRef = useRef<boolean>(false)

	const zoomed = scale > 1.05

	const captureBaseSize = () => {
		const img = imgRef.current
		if (!img) return
		const rect = img.getBoundingClientRect()
		if (rect.width > 0 && rect.height > 0) {
			setBaseSize({ width: rect.width, height: rect.height })
		}
	}

	const toggleZoom = () => {
		if (!zoomed) {
			if (!baseSize) captureBaseSize()
			setScale(ZOOM_TOGGLE_SCALE)
		} else {
			setScale(MIN_SCALE)
		}
	}

	useEffect(() => {
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.key === 'Escape') {
				onClose()
			}
		}
		window.addEventListener('keydown', handleKeyDown)
		return () => window.removeEventListener('keydown', handleKeyDown)
	}, [onClose])

	const handleTouchStart = (e: ReactTouchEvent<HTMLDivElement>) => {
		if (e.touches.length === 2) {
			if (!baseSize) captureBaseSize()
			pinchStartDistRef.current = getTouchDistance(e.touches)
			pinchStartScaleRef.current = scale
			pinchedDuringGestureRef.current = true
		}
	}

	const handleTouchMove = (e: ReactTouchEvent<HTMLDivElement>) => {
		if (e.touches.length !== 2 || pinchStartDistRef.current === null) return
		e.preventDefault()
		const dist = getTouchDistance(e.touches)
		const ratio = dist / Math.max(pinchStartDistRef.current, 1)
		const nextScale = Math.max(
			MIN_SCALE,
			Math.min(MAX_SCALE, Number((pinchStartScaleRef.current * ratio).toFixed(3)))
		)
		setScale(nextScale <= 1.03 ? MIN_SCALE : nextScale)
	}

	const handleTouchEnd = (e: ReactTouchEvent<HTMLDivElement>) => {
		if (e.touches.length < 2) {
			pinchStartDistRef.current = null
			if (e.touches.length === 0) {
				window.setTimeout(() => {
					pinchedDuringGestureRef.current = false
				}, 50)
			}
		}
	}

	const zoomedStyle =
		zoomed && baseSize
			? {
					width: `${Math.round(baseSize.width * scale)}px`,
					height: `${Math.round(baseSize.height * scale)}px`
				}
			: zoomed
			? {
					minWidth: `min(${Math.round(scale * 80)}vw, ${Math.round(scale * 600)}px)`
				}
			: undefined

	return (
		<div
			className="image-lightbox-backdrop"
			role="dialog"
			aria-modal="true"
			aria-label={alt || '画像プレビュー'}
			onClick={onClose}
		>
			<div className="image-lightbox-toolbar" onClick={(e) => e.stopPropagation()}>
				<button
					type="button"
					onClick={toggleZoom}
					className="image-lightbox-btn"
					aria-label={zoomed ? '元のサイズに戻す' : '画像を拡大する'}
				>
					{zoomed ? <Minimize2 size={18} aria-hidden="true" /> : <Maximize2 size={18} aria-hidden="true" />}
					<span>{zoomed ? '1x' : '2x'}</span>
				</button>
				<button
					type="button"
					onClick={onClose}
					className="image-lightbox-btn"
					aria-label="画像を閉じる"
				>
					<X size={20} aria-hidden="true" />
				</button>
			</div>

			<div
				className={`image-lightbox-stage ${zoomed ? 'zoomed' : ''}`}
				onClick={(e) => e.stopPropagation()}
				onTouchStart={handleTouchStart}
				onTouchMove={handleTouchMove}
				onTouchEnd={handleTouchEnd}
				onTouchCancel={handleTouchEnd}
			>
				<img
					ref={imgRef}
					src={src}
					alt={alt}
					style={zoomedStyle}
					className={`image-lightbox-img ${zoomed ? 'zoomed' : ''}`}
					onLoad={() => {
						if (!zoomed) captureBaseSize()
					}}
					onClick={() => {
						if (pinchedDuringGestureRef.current) return
						toggleZoom()
					}}
				/>
			</div>
		</div>
	)
}
