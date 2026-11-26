import { IonIcon } from '@ionic/react'
import { openOutline } from 'ionicons/icons'
import { extractExternalLinks, parseAlertDetails } from '../utils/url'
import type { UnifiedNotification } from '../types/notification'

interface NotificationDetailPanelProps {
	item: UnifiedNotification
	grafanaBase: string
}

// NotificationDetailPanel コンポーネントである。アラートのラベル/アノテーションや一般通知の詳細・外部リンクを表示する。
export default function NotificationDetailPanel({ item, grafanaBase }: NotificationDetailPanelProps) {
	const links = extractExternalLinks(item, grafanaBase)
	const parsedAlert = item.kind === 'alert' ? parseAlertDetails(item.details) : null

	const renderBody = () => {
		if (parsedAlert) {
			return (
				<>
					{parsedAlert.labels && (
						<div className="details-section">
							<h4 className="details-sec-title">Labels</h4>
							<div className="tag-container">
								{Object.entries(parsedAlert.labels).map(([k, v]) => (
									<span key={k} className="badge-tag">
										<strong>{k}:</strong> {v}
									</span>
								))}
							</div>
						</div>
					)}
					{parsedAlert.annotations && (
						<div className="details-section">
							<h4 className="details-sec-title">Annotations</h4>
							<table className="details-table">
								<tbody>
									{Object.entries(parsedAlert.annotations).map(([k, v]) => (
										<tr key={k}>
											<td className="details-key">{k}</td>
											<td className="details-val">{v}</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					)}
				</>
			)
		}

		if (item.description && item.description !== item.body) {
			return (
				<div className="details-section">
					<h4 className="details-sec-title">説明</h4>
					<p className="details-text">{item.description}</p>
				</div>
			)
		}

		if (!item.details) {
			return null
		}

		// 一般通知等で JSON が入っている場合は整形表示する
		try {
			const parsedJson = JSON.parse(item.details)
			return (
				<div className="details-raw">
					<pre className="log-code">{JSON.stringify(parsedJson, null, 2)}</pre>
				</div>
			)
		} catch {
			return (
				<div className="details-raw">
					<pre className="log-code">{item.details}</pre>
				</div>
			)
		}
	}

	return (
		<div className="history-detail-panel">
			<div className="details-parsed">
				{renderBody()}
				{links.length > 0 && (
					<div className="details-links-row">
						{links.map((link) => (
							<a
								key={link.url}
								href={link.url}
								target="_blank"
								rel="noopener noreferrer"
								className={`btn btn-${link.variant} btn-sm`}
							>
								<IonIcon icon={openOutline} aria-hidden="true" />
								{link.label}
							</a>
						))}
					</div>
				)}
			</div>
		</div>
	)
}
