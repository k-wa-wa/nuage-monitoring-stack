import { Button, Chip, EmptyState, ListItem, ListSection, Page, Segmented } from '@nuage/ui'
import { AlertTriangle, Bell, CheckCircle2, CircleAlert, XCircle } from 'lucide-react'
import { useActivity } from '../hooks/useActivity'
import { threadPath } from '../routes/paths'
import { formatMessageTime } from '../utils/date'
import type { ActivityFilter, ChatMessage } from '../types/chat'

function iconFor(m: ChatMessage) {
	if (m.source_kind === 'alert') {
		return m.alert_status === 'firing' ? <CircleAlert size={18} className="tone-warn" /> : <CheckCircle2 size={18} className="tone-success" />
	}
	switch (m.level) {
		case 'error':
			return <XCircle size={18} className="tone-warn" />
		case 'warning':
			return <AlertTriangle size={18} className="tone-blocked" />
		case 'success':
			return <CheckCircle2 size={18} className="tone-success" />
		default:
			return <Bell size={18} className="tone-accent" />
	}
}

// 本文 (Markdown) の 1 行目を見出し、残りを本文として、記号を落として出す
function summarize(text: string): { title: string; body: string } {
	const plain = (t: string) =>
		t
			.replace(/<[^>]+>/g, '')
			.replace(/[*_~`]/g, '')
			.replace(/^\s*[🔥✅]\s*/u, '')
			.replace(/^\[(firing|resolved)\]\s*/, '')
			.trim()
	const lines = text.split('\n').map(plain).filter(Boolean)
	return { title: lines[0] ?? '', body: lines.slice(1).join(' ') }
}

function durationLabel(firedAt?: string, resolvedAt?: string): string | null {
	if (!firedAt || !resolvedAt) return null
	const ms = new Date(resolvedAt).getTime() - new Date(firedAt).getTime()
	if (!(ms > 0)) return null
	const min = Math.floor(ms / 60000)
	if (min < 1) return `${Math.floor(ms / 1000)}秒で解決`
	if (min < 60) return `${min}分で解決`
	return `${Math.floor(min / 60)}時間${min % 60}分で解決`
}

const EMPTY_TEXT: Record<ActivityFilter, string> = {
	all: '通知はまだない。',
	firing: '発生中のアラートはない。',
	resolved: '解決済みのアラートはない。',
	notice: '一般の通知はない。'
}

export default function ActivityPage() {
	const { items, firingCount, filter, setFilter, loading, error, reload } = useActivity()
	const initialLoading = loading && items === null

	return (
		<Page
			title="通知"
			loading={initialLoading}
			loadingMessage="通知を読み込んでいる…"
			headerExtra={
				<Segmented
					label="通知の絞り込み"
					value={filter}
					onChange={setFilter}
					options={[
						{ value: 'all', label: 'すべて' },
						{ value: 'firing', label: `発生中${firingCount > 0 ? ` ${firingCount}` : ''}` },
						{ value: 'resolved', label: '解決済み' },
						{ value: 'notice', label: '一般' }
					]}
				/>
			}
		>
			{error && (!items || items.length === 0) ? (
				<EmptyState>
					<p>{error}</p>
					<div className="empty-action">
						<Button onClick={reload}>再読み込み</Button>
					</div>
				</EmptyState>
			) : items && items.length === 0 ? (
				<EmptyState>{EMPTY_TEXT[filter]}</EmptyState>
			) : items ? (
				<ListSection loading={loading}>
					{items.map(({ channel_id, channel_name, message: m }) => {
						const { title, body } = summarize(m.text)
						const resolvedIn = m.source_kind === 'alert' ? durationLabel(m.fired_at, m.resolved_at) : null
						return (
							<ListItem
								key={`${channel_id}-${m.ts}`}
								className="activity-item"
								to={threadPath('/tabs/activity', channel_id, m.ts)}
								start={iconFor(m)}
								title={
									<span className="activity-head">
										<span className="activity-title">{title}</span>
										{m.source_kind === 'alert' && m.alert_status && (
											<Chip tone={m.alert_status === 'firing' ? 'warn' : 'success'}>
												{m.alert_status === 'firing' ? '発生中' : '解決済み'}
											</Chip>
										)}
									</span>
								}
								subtitle={
									<span className="activity-sub">
										<span className="activity-source"># {channel_name}</span>
										{body ? ` · ${body}` : ''}
										{resolvedIn ? ` · ${resolvedIn}` : ''}
									</span>
								}
								end={<span className="activity-time">{formatMessageTime(m.fired_at || m.created_at)}</span>}
							/>
						)
					})}
				</ListSection>
			) : null}
		</Page>
	)
}
