// syncExistingPushSubscription はログイン後に既存 Push 購読をバックエンドへ再送信し、
// 現在のログインユーザーに紐づける。
export async function syncExistingPushSubscription(
	apiBase: string,
	sub: PushSubscription | object,
	onLog: (msg: string) => void,
	fetchFn: typeof fetch = fetch
): Promise<boolean> {
	try {
		const res = await fetchFn(`${apiBase}/api/subscribe`, {
			method: 'POST',
			credentials: 'include',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(sub)
		})
		if (!res.ok) {
			onLog(`購読情報の同期に失敗した: HTTP ${res.status}`)
			return false
		}
		return true
	} catch (err: unknown) {
		const message = err instanceof Error ? err.message : String(err)
		onLog(`購読情報の同期に失敗した: ${message}`)
		return false
	}
}

function urlBase64ToUint8Array(base64String: string): Uint8Array<ArrayBuffer> {
	const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
	const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
	const rawData = window.atob(base64)
	const output = new Uint8Array(new ArrayBuffer(rawData.length))
	for (let i = 0; i < rawData.length; ++i) {
		output[i] = rawData.charCodeAt(i)
	}
	return output
}

export function isPushSupported(): boolean {
	return 'serviceWorker' in navigator && 'PushManager' in window
}

// getCurrentSubscription は今の Push 購読を返し、あればログインユーザーに紐づけ直す。
export async function getCurrentSubscription(
	apiBase: string,
	onLog: (msg: string) => void
): Promise<PushSubscription | null> {
	if (!isPushSupported()) return null
	const reg = await navigator.serviceWorker.ready
	const sub = await reg.pushManager.getSubscription()
	if (sub) await syncExistingPushSubscription(apiBase, sub, onLog)
	return sub
}

// subscribePush は通知の許可を求め、購読してサーバーへ登録する。
export async function subscribePush(apiBase: string): Promise<void> {
	if (!isPushSupported()) {
		throw new Error('このブラウザは Web Push に対応していない (iOS はホーム画面に追加して開く必要がある)。')
	}
	const permission = await Notification.requestPermission()
	if (permission !== 'granted') throw new Error(`通知が許可されなかった: ${permission}`)
	const reg = await navigator.serviceWorker.ready
	const vapidRes = await fetch(`${apiBase}/api/vapid-public-key`, { credentials: 'include' })
	if (!vapidRes.ok) throw new Error('VAPID 公開鍵の取得に失敗した。')
	const { publicKey } = (await vapidRes.json()) as { publicKey: string }
	const sub = await reg.pushManager.subscribe({
		userVisibleOnly: true,
		applicationServerKey: urlBase64ToUint8Array(publicKey)
	})
	const saveRes = await fetch(`${apiBase}/api/subscribe`, {
		method: 'POST',
		credentials: 'include',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(sub)
	})
	if (!saveRes.ok) throw new Error('サーバーへの購読登録に失敗した。')
}

// unsubscribePush は購読を解除し、サーバーからも消す。
export async function unsubscribePush(apiBase: string): Promise<void> {
	const reg = await navigator.serviceWorker.ready
	const sub = await reg.pushManager.getSubscription()
	if (!sub) return
	await sub.unsubscribe()
	await fetch(`${apiBase}/api/unsubscribe`, {
		method: 'POST',
		credentials: 'include',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ endpoint: sub.endpoint })
	})
}

// sendTestNotify はテスト通知の送信をサーバーへ頼む。
export async function sendTestNotify(apiBase: string): Promise<void> {
	const res = await fetch(`${apiBase}/api/test-notify`, {
		method: 'POST',
		credentials: 'include',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			title: 'テスト通知',
			body: 'これは Nuage Notify からのテスト通知である。',
			level: 'success'
		})
	})
	if (!res.ok) throw new Error(`送信要求に失敗した: HTTP ${res.status}`)
}
