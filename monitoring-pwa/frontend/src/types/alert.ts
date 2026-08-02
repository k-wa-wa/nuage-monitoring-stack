export interface AlertItem {
	id: number
	fingerprint: string
	alertname: string
	status: 'firing' | 'resolved'
	level: 'info' | 'success' | 'warning' | 'error'
	summary: string
	description?: string
	url?: string
	details?: string
	fired_at: string
	resolved_at?: string
	created_at: string
	updated_at: string
}
