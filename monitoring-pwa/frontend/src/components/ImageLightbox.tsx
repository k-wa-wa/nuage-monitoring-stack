import { useEffect, useState } from 'react'
import { X, ZoomIn, ZoomOut } from 'lucide-react'

interface ImageLightboxProps {
	src: string
	alt: string
	onClose: () => void
}

/**
 * スマホ等でピンチ拡大を無効化した環境でも画像の詳細を読めるようにする全画面プレビューである。
 */
export default function ImageLightbox({ src, alt, onClose }: ImageLightboxProps) {
	const [zoomed, setZoomed] = useState(false)

	useEffect(() => {
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.key === 'Escape') {
				onClose()
			}
		}
		window.addEventListener('keydown', handleKeyDown)
		return () => window.removeEventListener('keydown', handleKeyDown)
	}, [onClose])

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
					onClick={() => setZoomed((prev) => !prev)}
					className="image-lightbox-btn"
					aria-label={zoomed ? '元のサイズに戻す' : '画像を拡大する'}
				>
					{zoomed ? <ZoomOut size={18} /> : <ZoomIn size={18} />}
					<span>{zoomed ? '1x' : '2x'}</span>
				</button>
				<button
					type="button"
					onClick={onClose}
					className="image-lightbox-btn"
					aria-label="画像を閉じる"
				>
					<X size={20} />
				</button>
			</div>

			<div
				className={`image-lightbox-stage ${zoomed ? 'zoomed' : ''}`}
				onClick={(e) => e.stopPropagation()}
			>
				<img
					src={src}
					alt={alt}
					className={`image-lightbox-img ${zoomed ? 'zoomed' : ''}`}
					onClick={() => setZoomed((prev) => !prev)}
				/>
			</div>
		</div>
	)
}
