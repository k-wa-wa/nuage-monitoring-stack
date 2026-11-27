import { useCallback, useState } from 'react'
import { loginWithPassphrase } from '../api/auth'
import { useChatActions } from '../state/chatStore'

/** ログイン送信状態とエラーメッセージを管理する。 */
export function useLogin() {
	const { reloadBootstrap } = useChatActions()
	const [submitting, setSubmitting] = useState(false)
	const [error, setError] = useState<string | null>(null)

	const login = useCallback(
		async (passphrase: string): Promise<boolean> => {
			if (!passphrase.trim() || submitting) return false
			setSubmitting(true)
			setError(null)
			try {
				const res = await loginWithPassphrase(passphrase)
				if (!res.ok) {
					setError(res.error)
					return false
				}
				await reloadBootstrap()
				return true
			} finally {
				setSubmitting(false)
			}
		},
		[reloadBootstrap, submitting]
	)

	return { submitting, error, login }
}
