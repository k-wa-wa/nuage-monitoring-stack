// GeneratorURLの書き換え（クラスタ内URLをPWA公開用URLへ置換）である。
export const rewriteGeneratorUrl = (url: string, base: string) => {
	if (!url) return ''
	return url.replace(/http:\/\/.*\.svc\.cluster\.local(:\d+)?/, `${base}/grafana`)
}

// LabelsからVictoriaLogsのログリンク（Grafana Explore）を生成する。
// アラートのラベル (namespace / pod / container) を VictoriaLogs の Kubernetes メタデータのフィールドに対応させる。
export const getLogsUrl = (labels: Record<string, string>, base: string) => {
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

	const exploreState = {
		datasource: 'VictoriaLogs',
		queries: [{ refId: 'A', expr: parts.join(' ') }],
		range: { from: 'now-1h', to: 'now' }
	}
	const encoded = encodeURIComponent(JSON.stringify(exploreState))
	return `${base}/grafana/explore?left=${encoded}`
}
