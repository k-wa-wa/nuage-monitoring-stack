import type { ExternalLink, UnifiedNotification } from '../types/notification'

// Grafana の本番ベース URL。preview 環境から開いた場合も本番 Grafana に遷移させるため固定する。
export const GRAFANA_BASE_URL = 'https://monitoring.wpcapp.net'

// http / https スキームのみ許可して安全な外部 URL を返す。
export const sanitizeExternalUrl = (raw?: string | null): string | null => {
	if (!raw) return null
	const trimmed = raw.trim()
	if (!trimmed) return null
	try {
		const parsed = new URL(trimmed)
		if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
			return parsed.href
		}
		return null
	} catch {
		return null
	}
}

// GeneratorURL の書き換え（クラスタ内 URL を PWA 公開用 Grafana URL へ置換）である。
export const rewriteGeneratorUrl = (url: string, base: string = GRAFANA_BASE_URL): string => {
	if (!url) return ''
	const normalizedBase = base.replace(/\/+$/, '')

	if (url.startsWith('/')) {
		return sanitizeExternalUrl(`${normalizedBase}/grafana${url}`) || ''
	}

	// クラスタ内ホスト (*.svc.cluster.local, *.svc, またはドット無しの単一サービス名 http://grafana/...) を置換する
	const rewritten = url.replace(
		/^https?:\/\/([^/:]+\.svc(?:\.cluster\.local)?|[^./:]+)(?::\d+)?(\/.*)?$/,
		(_match, _host, path = '') => `${normalizedBase}/grafana${path}`
	)

	return sanitizeExternalUrl(rewritten) || ''
}

// Labels から VictoriaLogs のログリンク（Grafana Explore）を生成する。
// アラートのラベル (namespace / pod / container) を VictoriaLogs の Kubernetes メタデータのフィールドに対応させる。
export const getLogsUrl = (labels: Record<string, string>, base: string = GRAFANA_BASE_URL): string | null => {
	if (!labels) return null
	const parts: string[] = []
	if (labels.namespace) {
		parts.push(`kubernetes.pod_namespace:="${labels.namespace}"`)
	}
	if (labels.pod) {
		parts.push(`kubernetes.pod_name:="${labels.pod}"`)
	} else if (labels.app) {
		parts.push(`kubernetes.pod_labels.app:="${labels.app}"`)
	}
	if (labels.container) {
		parts.push(`kubernetes.container_name:="${labels.container}"`)
	}

	if (parts.length === 0) return null

	const normalizedBase = base.replace(/\/+$/, '')
	const exploreState = {
		datasource: 'VictoriaLogs',
		queries: [{ refId: 'A', expr: parts.join(' ') }],
		range: { from: 'now-1h', to: 'now' }
	}
	const encoded = encodeURIComponent(JSON.stringify(exploreState))
	return `${normalizedBase}/grafana/explore?left=${encoded}`
}

export interface AlertDetailsParsed {
	labels?: Record<string, string>
	annotations?: Record<string, string>
	generatorURL?: string
}

export const parseAlertDetails = (details?: string): AlertDetailsParsed | null => {
	if (!details) return null
	try {
		const parsed = JSON.parse(details) as AlertDetailsParsed
		if (parsed && typeof parsed === 'object' && (parsed.labels || parsed.annotations || parsed.generatorURL)) {
			return parsed
		}
		return null
	} catch {
		return null
	}
}

// 通知アイテムから遷移可能な外部アプリのリンク一覧を抽出する。
export const extractExternalLinks = (
	item: UnifiedNotification,
	grafanaBase: string = GRAFANA_BASE_URL
): ExternalLink[] => {
	const links: ExternalLink[] = []
	const seenUrls = new Set<string>()

	const pushLink = (label: string, rawUrl: string | null | undefined, variant: 'primary' | 'secondary') => {
		const safe = sanitizeExternalUrl(rawUrl)
		if (!safe || seenUrls.has(safe)) return
		seenUrls.add(safe)
		links.push({ label, url: safe, variant })
	}

	if (item.kind === 'alert') {
		const parsed = parseAlertDetails(item.details)
		const logsUrl = parsed?.labels ? getLogsUrl(parsed.labels, grafanaBase) : null
		if (logsUrl) {
			pushLink('ログを確認 (Grafana)', logsUrl, 'primary')
		}

		const rawGenerator = parsed?.generatorURL || item.url
		if (rawGenerator) {
			const genUrl = rewriteGeneratorUrl(rawGenerator, grafanaBase)
			const label = genUrl.includes('/grafana/') ? 'Grafanaで確認' : 'Prometheusで確認'
			pushLink(label, genUrl, logsUrl ? 'secondary' : 'primary')
		}

		const dashboardUrl = parsed?.annotations?.dashboard_url
		if (dashboardUrl) {
			pushLink('ダッシュボードを開く', dashboardUrl, 'secondary')
		}

		const runbookUrl = parsed?.annotations?.runbook_url
		if (runbookUrl) {
			pushLink('Runbookを開く', runbookUrl, 'secondary')
		}
		return links
	}

	// 一般通知 (Argo CD / Keel / 汎用 Webhook)
	if (item.url) {
		pushLink('対象アプリを開く', item.url, 'primary')
	}
	return links
}

export interface ParsedAppRoute {
	tab: 'notifications' | 'settings'
	selectedUid: string | null
	detailKind: 'alert' | 'notification' | null
	detailId: number | null
	legacyHistoryId: number | null
}

// URL パスをアプリ内のタブおよび選択通知状態へパースする。
export const parseAppRoute = (pathname: string): ParsedAppRoute => {
	if (pathname === '/settings' || pathname.startsWith('/settings/')) {
		return {
			tab: 'settings',
			selectedUid: null,
			detailKind: null,
			detailId: null,
			legacyHistoryId: null
		}
	}

	const alertMatch = pathname.match(/^\/alerts\/(\d+)\/?$/)
	if (alertMatch) {
		const id = parseInt(alertMatch[1], 10)
		return {
			tab: 'notifications',
			selectedUid: `alert-${id}`,
			detailKind: 'alert',
			detailId: id,
			legacyHistoryId: null
		}
	}

	const noteMatch = pathname.match(/^\/notifications\/(\d+)\/?$/)
	if (noteMatch) {
		const id = parseInt(noteMatch[1], 10)
		return {
			tab: 'notifications',
			selectedUid: `notification-${id}`,
			detailKind: 'notification',
			detailId: id,
			legacyHistoryId: null
		}
	}

	const legacyMatch = pathname.match(/^\/history\/(\d+)\/?$/)
	if (legacyMatch) {
		const id = parseInt(legacyMatch[1], 10)
		return {
			tab: 'notifications',
			selectedUid: null,
			detailKind: null,
			detailId: null,
			legacyHistoryId: id
		}
	}

	return {
		tab: 'notifications',
		selectedUid: null,
		detailKind: null,
		detailId: null,
		legacyHistoryId: null
	}
}
