import { AlertTriangle, CheckCircle, Clock, ArrowRight } from 'lucide-react'
import HistoryDetailPanel from './HistoryDetailPanel'
import { formatHistoryTimestamp } from '../utils/date'
import type { AlertItem } from '../types/alert'

interface HistoryItemProps {
	item: AlertItem
	isExpanded: boolean
	onToggle: () => void
	grafanaBase: string
}

// 解決までの所要時間を計算・整形するヘルパーである。
function getDurationString(firedAtStr: string, resolvedAtStr?: string): string | null {
	if (!resolvedAtStr) return null
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

// HistoryItem コンポーネントである。スレッド化されたアラート履歴（発生〜解決の経過）を表示する。
export default function HistoryItem({ item, isExpanded, onToggle, grafanaBase }: HistoryItemProps) {
	const isFiring = item.status === 'firing'
	const duration = getDurationString(item.fired_at, item.resolved_at)

	return (
		<div
			id={`history-item-${item.id}`}
			className={`history-item ${isFiring ? 'status-firing' : 'status-resolved'} ${isExpanded ? 'expanded-active' : ''}`}
		>
			<div className="history-icon-wrapper">
				{isFiring ? (
					<AlertTriangle size={18} className="icon-firing-thread" />
				) : (
					<CheckCircle size={18} className="icon-resolved-thread" />
				)}
			</div>

			<div className="history-content">
				<div className="history-header">
					<div className="history-title-group">
						<span className="history-item-title">{item.alertname || item.summary}</span>
						<span className={`status-badge ${isFiring ? 'badge-firing' : 'badge-resolved'}`}>
							{isFiring ? 'FIRING' : 'RESOLVED'}
						</span>
					</div>
				</div>

				<p className="history-body">{item.summary}</p>

				{/* 発生〜解決のタイムライン表示 */}
				<div className="history-timeline-bar">
					<div className="timeline-point">
						<Clock size={12} />
						<span>発生: {formatHistoryTimestamp(item.fired_at)}</span>
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

				<div className="history-actions">
					{item.details && (
						<button onClick={onToggle} className="history-detail-toggle-btn">
							{isExpanded ? '詳細を閉じる' : '詳細を表示'}
						</button>
					)}
					{item.url && !item.details && (
						<a href={item.url} target="_blank" rel="noopener noreferrer" className="history-link">
							詳細を表示
						</a>
					)}
				</div>

				{isExpanded && item.details && (
					<HistoryDetailPanel details={item.details} grafanaBase={grafanaBase} />
				)}
			</div>
		</div>
	)
}

