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
