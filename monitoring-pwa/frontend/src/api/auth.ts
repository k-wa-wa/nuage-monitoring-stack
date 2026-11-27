import type { BootstrapResponse } from '../types/chat'
import { API_BASE, ApiError, apiFetch, type UnauthorizedHandler } from './client'

export type LoginResult =
	| { ok: true }
	| { ok: false; error: string }

/** 合言葉でログインする。 */
export async function loginWithPassphrase(passphrase: string): Promise<LoginResult> {
	try {
		const res = await fetch(`${API_BASE}/api/auth/login`, {
			method: 'POST',
			credentials: 'include',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ passphrase })
		})
		if (!res.ok) {
			return {
				ok: false,
				error:
					res.status === 429
						? 'ログインの試行が多すぎる。少し待ってからもう一度試してほしい。'
						: res.status === 401
							? '合言葉が正しくない。'
							: `ログインに失敗した (HTTP ${res.status})。`
			}
		}
		return { ok: true }
	} catch {
		return { ok: false, error: 'サーバーにつながらなかった。' }
	}
}

/** セッションからログアウトする。 */
export async function logoutSession(): Promise<void> {
	await fetch(`${API_BASE}/api/auth/logout`, { method: 'POST', credentials: 'include' })
}

/** ログイン状態・ユーザー・チャンネル一覧・bot 情報を取得する。 */
export async function fetchBootstrap(onUnauthorized?: UnauthorizedHandler): Promise<BootstrapResponse | null> {
	const res = await apiFetch('/api/bootstrap', undefined, onUnauthorized)
	if (!res) return null
	if (!res.ok) throw new ApiError(res.status)
	return (await res.json()) as BootstrapResponse
}
