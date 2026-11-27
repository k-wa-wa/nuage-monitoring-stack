import { useSyncExternalStore } from 'react'

function subscribeMedia(query: string) {
	return (onChange: () => void) => {
		if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => {}
		const mql = window.matchMedia(query)
		mql.addEventListener('change', onChange)
		return () => mql.removeEventListener('change', onChange)
	}
}

/** CSS のメディアクエリに当てはまるかを返す。サーバー側の描画 (SSR) では serverValue を返す。 */
export function useMediaQuery(query: string, serverValue = false): boolean {
	return useSyncExternalStore(
		subscribeMedia(query),
		() => (typeof window !== 'undefined' && typeof window.matchMedia === 'function' ? window.matchMedia(query).matches : serverValue),
		() => serverValue
	)
}

/** PC・タブレット (横に並べて表示できる幅) か。 */
export const WIDE_QUERY = '(min-width: 900px)'
export function useIsWide(): boolean {
	return useMediaQuery(WIDE_QUERY)
}

/** 動きを減らす設定か。 */
export function usePrefersReducedMotion(): boolean {
	return useMediaQuery('(prefers-reduced-motion: reduce)')
}

/** ホーム画面に追加して開いた PWA か。 */
export function useIsStandalone(): boolean {
	const media = useMediaQuery('(display-mode: standalone)')
	const iosStandalone =
		typeof navigator !== 'undefined' && (navigator as Navigator & { standalone?: boolean }).standalone === true
	return media || iosStandalone
}
