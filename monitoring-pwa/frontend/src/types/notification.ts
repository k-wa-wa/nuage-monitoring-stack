export interface UnifiedNotification {
	uid: string
	id: number
	kind: 'alert' | 'notification'
	source: string
	title: string
	body: string
	description?: string
	status: 'firing' | 'resolved' | 'notice'
	level: 'info' | 'success' | 'warning' | 'error'
	url?: string
	details?: string
	timestamp: string
	fired_at?: string
	resolved_at?: string
	created_at: string
	updated_at: string
}

export interface ExternalLink {
	label: string
	url: string
	variant: 'primary' | 'secondary'
}
