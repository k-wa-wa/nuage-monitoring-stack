import { rewriteGeneratorUrl, getLokiUrl } from '../utils/url'

interface HistoryDetailPanelProps {
	details: string
	grafanaBase: string
}

// HistoryDetailPanel コンポーネントである。エラー履歴の詳細やLokiログリンクを表示する。
export default function HistoryDetailPanel({ details, grafanaBase }: HistoryDetailPanelProps) {
	try {
		const parsed = JSON.parse(details) as {
			labels?: Record<string, string>
			annotations?: Record<string, string>
			generatorURL?: string
		}

		if (parsed.labels || parsed.annotations) {
			const lokiUrl = getLokiUrl(parsed.labels || {}, grafanaBase)
			return (
				<div className="details-parsed">
					{parsed.labels && (
						<div className="details-section">
							<h4 className="details-sec-title">Labels</h4>
							<div className="tag-container">
								{Object.entries(parsed.labels).map(([k, v]) => (
									<span key={k} className="badge-tag">
										<strong>{k}:</strong> {v}
									</span>
								))}
							</div>
						</div>
					)}
					{parsed.annotations && (
						<div className="details-section">
							<h4 className="details-sec-title">Annotations</h4>
							<table className="details-table">
								<tbody>
									{Object.entries(parsed.annotations).map(([k, v]) => (
										<tr key={k}>
											<td className="details-key">{k}</td>
											<td className="details-val">{v}</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					)}
					<div className="details-links-row">
						{parsed.generatorURL && (
							<a
								href={rewriteGeneratorUrl(parsed.generatorURL, grafanaBase)}
								target="_blank"
								rel="noopener noreferrer"
								className="btn btn-secondary btn-sm"
							>
								Prometheusで確認
							</a>
						)}
						{lokiUrl && (
							<a
								href={lokiUrl}
								target="_blank"
								rel="noopener noreferrer"
								className="btn btn-primary btn-sm"
							>
								Lokiのログを確認 (Grafana)
							</a>
						)}
					</div>
				</div>
			)
		}
	} catch (e) {
		// JSON パースエラー、またはプレーンテキスト
	}

	// プレーンテキスト（エラーログ）の場合
	return (
		<div className="details-raw">
			<pre className="log-code">{details}</pre>
		</div>
	)
}
