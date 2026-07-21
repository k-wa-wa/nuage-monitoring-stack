import { AlertTriangle, CheckCircle, Bell } from 'lucide-react'
import HistoryDetailPanel from './HistoryDetailPanel'

interface NotificationItem {
	id: number
	title: string
	body: string
	url?: string
	level: 'info' | 'success' | 'warning' | 'error'
	details?: string
	created_at: string
}

interface HistoryItemProps {
	item: NotificationItem
	isExpanded: boolean
	onToggle: () => void
	grafanaBase: string
}

// HistoryItem コンポーネントである。履歴レコード表示と詳細パネルの開閉を管理する。
export default function HistoryItem({ item, isExpanded, onToggle, grafanaBase }: HistoryItemProps) {
	return (
		<div
			id={`history-item-${item.id}`}
			className={`history-item ${item.level} ${isExpanded ? 'expanded-active' : ''}`}
		>
			<div className="history-icon-wrapper">
				{item.level === 'error' && <AlertTriangle size={16} />}
				{item.level === 'warning' && <AlertTriangle size={16} />}
				{item.level === 'success' && <CheckCircle size={16} />}
				{item.level === 'info' && <Bell size={16} />}
			</div>
			<div className="history-content">
				<div className="history-header">
					<span className="history-item-title">{item.title}</span>
					<span className="history-time">{new Date(item.created_at).toLocaleTimeString()}</span>
				</div>
				<span className="history-body">{item.body}</span>
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
