import {
	AlertTriangle,
	Bell,
	CheckCircle,
	Clock,
	ArrowRight,
	Info,
	XCircle,
	ExternalLink as ExternalLinkIcon
} from 'lucide-react'
import NotificationDetailPanel from './NotificationDetailPanel'
import { formatHistoryTimestamp } from '../utils/date'
import { extractExternalLinks } from '../utils/url'
import type { UnifiedNotification } from '../types/notification'

interface NotificationItemProps {
	item: UnifiedNotification
	isExpanded: boolean
	onToggle: () => void
	grafanaBase: string
}

// 解決までの所要時間を計算・整形するヘルパーである。
function getDurationString(firedAtStr?: string, resolvedAtStr?: string): string | null {
	if (!firedAtStr || !resolvedAtStr) return null
	const fired = new Date(firedAtStr).getTime()
	const resolved = new Date(resolvedAtStr).getTime()
	if (isNaN(fired) || isNaN(resolved) || resolved <= fired) return null

	const diffSec = Math.floor((resolved - fired) / 1000)
	if (diffSec < 60) return `${diffSec}秒`
	const diffMin = Math.floor(diffSec / 60)
	if (diffMin < 60) return `${diffMin}分`
	const diffHours = Math.floor(diffMin / 60)
	const remainMin = diffMin % 60
	return `${diffHours}時間${remainMin}分`
}

function getSourceLabel(item: UnifiedNotification): string {
	switch (item.source) {
		case 'alertmanager':
			return 'ALERT'
		case 'keel':
			return 'KEEL'
		case 'argocd':
			return 'ARGO CD'
		default:
			return 'WEBHOOK'
	}
}

function renderStatusIcon(item: UnifiedNotification) {
	if (item.kind === 'alert') {
		return item.status === 'firing' ? (
			<AlertTriangle size={18} className="icon-firing-thread" />
		) : (
			<CheckCircle size={18} className="icon-resolved-thread" />
		)
	}
	switch (item.level) {
		case 'error':
			return <XCircle size={18} />
		case 'warning':
			return <AlertTriangle size={18} />
		case 'success':
			return <CheckCircle size={18} />
		case 'info':
			return <Info size={18} />
		default:
			return <Bell size={18} />
	}
}

// NotificationItem コンポーネントである。アラート（発生〜解決スレッド）と一般通知を統合表示する。
export default function NotificationItem({ item, isExpanded, onToggle, grafanaBase }: NotificationItemProps) {
	const isAlert = item.kind === 'alert'
	const isFiring = item.status === 'firing'
	const duration = getDurationString(item.fired_at, item.resolved_at)
	const externalLinks = extractExternalLinks(item, grafanaBase)
	const hasExpandableDetail = Boolean(
		item.details || (item.description && item.description !== item.body)
	)

	const itemStatusClass = isAlert
		? isFiring
			? 'status-firing'
			: 'status-resolved'
		: item.level

	return (
		<div
			id={`notification-item-${item.uid}`}
			className={`history-item ${itemStatusClass} ${isExpanded ? 'expanded-active' : ''}`}
		>
			<div className="history-icon-wrapper">{renderStatusIcon(item)}</div>

			<div className="history-content">
				<div className="history-header">
					<div className="history-title-group">
						<span className="history-item-title">{item.title || item.body}</span>
						{isAlert ? (
							<span className={`status-badge ${isFiring ? 'badge-firing' : 'badge-resolved'}`}>
								{isFiring ? 'FIRING' : 'RESOLVED'}
							</span>
						) : (
							<span className={`status-badge badge-source badge-source-${item.source}`}>
								{getSourceLabel(item)}
							</span>
						)}
					</div>
				</div>

				{item.body && item.body !== item.title && <p className="history-body">{item.body}</p>}

				{isAlert ? (
					<div className="history-timeline-bar">
						<div className="timeline-point">
							<Clock size={12} />
							<span>発生: {formatHistoryTimestamp(item.fired_at || item.created_at)}</span>
						</div>
						{item.resolved_at && (
							<>
								<ArrowRight size={12} className="timeline-arrow" />
								<div className="timeline-point resolved">
									<CheckCircle size={12} />
									<span>解決: {formatHistoryTimestamp(item.resolved_at)}</span>
								</div>
								{duration && <span className="duration-tag">({duration}で復旧)</span>}
							</>
						)}
						{isFiring && <span className="duration-tag firing-tag">継続中</span>}
					</div>
				) : (
					<div className="history-timeline-bar">
						<div className="timeline-point">
							<Clock size={12} />
							<span>受信: {formatHistoryTimestamp(item.created_at)}</span>
						</div>
					</div>
				)}

				{(hasExpandableDetail || externalLinks.length > 0) && (
					<div className="history-actions">
						{hasExpandableDetail && (
							<button onClick={onToggle} className="history-detail-toggle-btn">
								{isExpanded ? '詳細を閉じる' : '詳細を表示'}
							</button>
						)}
						{!isExpanded &&
							externalLinks.slice(0, 2).map((link) => (
								<a
									key={link.url}
									href={link.url}
									target="_blank"
									rel="noopener noreferrer"
									className="history-link"
								>
									<ExternalLinkIcon size={12} />
									{link.label}
								</a>
							))}
					</div>
				)}

				{isExpanded && (
					<NotificationDetailPanel item={item} grafanaBase={grafanaBase} />
				)}
			</div>
		</div>
	)
}
