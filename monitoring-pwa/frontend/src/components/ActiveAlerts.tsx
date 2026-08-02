import { AlertTriangle, CheckCircle2, ShieldAlert, ChevronRight } from 'lucide-react'
import type { AlertItem } from '../types/alert'
import { formatHistoryTimestamp } from '../utils/date'

interface ActiveAlertsProps {
	alerts: AlertItem[]
}

// ActiveAlerts コンポーネントである。現在 firing 状態のアラート一覧を表示する。
export default function ActiveAlerts({ alerts }: ActiveAlertsProps) {
	if (alerts.length === 0) {
		return (
			<div className="card active-alerts-card status-healthy">
				<div className="active-alerts-header">
					<div className="active-alerts-title healthy">
						<CheckCircle2 size={20} className="icon-healthy" />
						<span>システム正常</span>
					</div>
					<span className="active-alerts-badge badge-healthy">0件の発火</span>
				</div>
				<p className="active-alerts-subtext">現在、アクティブなアラートは発生していません。</p>
			</div>
		)
	}

	return (
		<div className="card active-alerts-card status-firing">
			<div className="active-alerts-header">
				<div className="active-alerts-title firing">
					<ShieldAlert size={20} className="icon-firing pulse-animation" />
					<span>アクティブアラート ({alerts.length}件)</span>
				</div>
				<span className="active-alerts-badge badge-firing">FIRING</span>
			</div>
			<div className="active-alerts-list">
				{alerts.map((alert) => (
					<div key={alert.id} className={`active-alert-item level-${alert.level}`}>
						<div className="active-alert-item-header">
							<div className="active-alert-name-wrapper">
								<AlertTriangle size={16} className="active-alert-icon" />
								<strong className="active-alert-name">{alert.alertname}</strong>
							</div>
							<span className="active-alert-time">{formatHistoryTimestamp(alert.fired_at)}</span>
						</div>
						<p className="active-alert-summary">{alert.summary}</p>
						{alert.description && alert.description !== alert.summary && (
							<p className="active-alert-description">{alert.description}</p>
						)}
						<div className="active-alert-actions">
							{alert.url && (
								<a
									href={alert.url}
									target="_blank"
									rel="noopener noreferrer"
									className="active-alert-link"
								>
									Generator / Grafana で確認
									<ChevronRight size={12} />
								</a>
							)}
						</div>
					</div>
				))}
			</div>
		</div>
	)
}
