// 通知履歴のタイムスタンプ表示（日付+時刻）を生成する。
export const formatHistoryTimestamp = (isoString: string) => {
	const date = new Date(isoString)
	const dateStr = date.toLocaleDateString('ja-JP', { month: '2-digit', day: '2-digit' })
	const timeStr = date.toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })
	return `${dateStr} ${timeStr}`
}
