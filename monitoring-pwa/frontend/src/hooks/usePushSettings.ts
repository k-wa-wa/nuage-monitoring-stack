import { useCallback, useEffect, useState } from 'react'
import { getCurrentSubscription, sendTestNotify, subscribePush, unsubscribePush } from '../api/push'
import { isPushSupported } from '../utils/pushSupport'

/** Web Push 購読の確認・切り替え・テスト送信を管理する。 */
export function usePushSettings(onToast: (message: string, tone?: 'success' | 'warn') => void) {
	const pushSupported = isPushSupported()
	const [subscribed, setSubscribed] = useState(false)
	const [checking, setChecking] = useState(pushSupported)
	const [busy, setBusy] = useState(false)
	const [sendingTest, setSendingTest] = useState(false)
	const [logs, setLogs] = useState<string[]>([])

	const log = useCallback((msg: string) => {
		setLogs((prev) => [`[${new Date().toLocaleTimeString()}] ${msg}`, ...prev].slice(0, 50))
	}, [])

	useEffect(() => {
		if (!pushSupported) return
		let cancelled = false
		getCurrentSubscription(log)
			.then((sub) => {
				if (!cancelled) setSubscribed(Boolean(sub))
			})
			.catch((err: unknown) => {
				if (!cancelled) log(`購読の確認に失敗した: ${err instanceof Error ? err.message : String(err)}`)
			})
			.finally(() => {
				if (!cancelled) setChecking(false)
			})
		return () => {
			cancelled = true
		}
	}, [log, pushSupported])

	const togglePush = useCallback(
		async (enable: boolean) => {
			if (checking || busy) return
			setBusy(true)
			try {
				if (enable) {
					await subscribePush()
					setSubscribed(true)
					onToast('通知を受け取るように登録した。', 'success')
				} else {
					await unsubscribePush()
					setSubscribed(false)
					onToast('通知の登録を解除した。')
				}
			} catch (err: unknown) {
				const message = err instanceof Error ? err.message : String(err)
				log(`エラー: ${message}`)
				onToast(message, 'warn')
			} finally {
				setBusy(false)
			}
		},
		[busy, checking, log, onToast]
	)

	const sendTest = useCallback(async () => {
		if (sendingTest) return
		setSendingTest(true)
		try {
			await sendTestNotify()
			onToast('テスト通知の送信を頼んだ。', 'success')
		} catch (err: unknown) {
			const message = err instanceof Error ? err.message : String(err)
			log(`エラー: ${message}`)
			onToast(message, 'warn')
		} finally {
			setSendingTest(false)
		}
	}, [log, onToast, sendingTest])

	return {
		pushSupported,
		subscribed,
		checking,
		busy,
		sendingTest,
		logs,
		togglePush,
		sendTest
	}
}
