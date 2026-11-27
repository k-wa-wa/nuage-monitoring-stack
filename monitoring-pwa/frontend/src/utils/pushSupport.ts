// Web Push のブラウザ対応判定と VAPID 鍵変換の純粋ヘルパーである。

export function isPushSupported(): boolean {
	return typeof navigator !== 'undefined' && typeof window !== 'undefined' && 'serviceWorker' in navigator && 'PushManager' in window
}

export function urlBase64ToUint8Array(base64String: string): Uint8Array<ArrayBuffer> {
	const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
	const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
	const rawData = window.atob(base64)
	const output = new Uint8Array(new ArrayBuffer(rawData.length))
	for (let i = 0; i < rawData.length; ++i) {
		output[i] = rawData.charCodeAt(i)
	}
	return output
}
