// GeneratorURLの書き換え（クラスタ内URLをPWA公開用URLへ置換）である。
export const rewriteGeneratorUrl = (url: string, base: string) => {
	if (!url) return ''
	return url.replace(/http:\/\/.*\.svc\.cluster\.local(:\d+)?/, `${base}/grafana`)
}

// LabelsからLokiログリンク（Grafana Explore）を生成する。
export const getLokiUrl = (labels: Record<string, string>, base: string) => {
	if (!labels) return null
	const parts: string[] = []
	if (labels.namespace) {
		parts.push(`namespace="${labels.namespace}"`)
	}
	if (labels.pod) {
		parts.push(`pod="${labels.pod}"`)
	} else if (labels.app) {
		parts.push(`app="${labels.app}"`)
	}
	if (labels.container) {
		parts.push(`container="${labels.container}"`)
	}

	if (parts.length === 0) return null

	const logql = `{${parts.join(', ')}}`
	const exploreState = [
		'now-1h',
		'now',
		'Loki',
		{
			expr: logql
		}
	]
	const encoded = encodeURIComponent(JSON.stringify(exploreState))
	return `${base}/grafana/explore?left=${encoded}`
}
