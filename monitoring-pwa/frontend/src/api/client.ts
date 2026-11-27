// ローカル開発時は別ポートで動くバックエンドを指す。
export const API_BASE = import.meta.env.DEV ? 'http://localhost:8080' : ''

export class ApiError extends Error {
	status: number

	constructor(status: number, message = `HTTP ${status}`) {
		super(message)
		this.name = 'ApiError'
		this.status = status
	}
}

export type UnauthorizedHandler = () => void

/**
 * 認証 Cookie 付きで API を呼び出す共通窓口である。
 * 401 のときは onUnauthorized を呼んで null を返す。
 */
export async function apiFetch(
	path: string,
	init?: RequestInit,
	onUnauthorized?: UnauthorizedHandler
): Promise<Response | null> {
	const res = await fetch(`${API_BASE}${path}`, { credentials: 'include', ...init })
	if (res.status === 401) {
		onUnauthorized?.()
		return null
	}
	return res
}
