// 通知履歴のタイムスタンプ表示（日付+時刻）を生成する。
export const formatHistoryTimestamp = (isoString: string) => {
	const date = new Date(isoString)
	const dateStr = date.toLocaleDateString('ja-JP', { month: '2-digit', day: '2-digit' })
	const timeStr = date.toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })
	return `${dateStr} ${timeStr}`
}

const pad2 = (n: number) => String(n).padStart(2, '0')

// formatMessageTime はメッセージ行の時刻を Slack と同じく短く出す。
// 今日なら「14:05」、今年なら「11/26 14:05」、それより前は「2025/11/26 14:05」。
// timeOnly は連続投稿の左端に出す時刻用で、常に「14:05」だけを返す。
export const formatMessageTime = (isoString: string, timeOnly = false, now: Date = new Date()) => {
	const d = new Date(isoString)
	if (isNaN(d.getTime())) return ''
	const time = `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
	if (timeOnly) return time
	const sameDay =
		d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
	if (sameDay) return time
	if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}/${d.getDate()} ${time}`
	return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()} ${time}`
}

// dayKey は日付の区切り線を入れるための日単位のキーを返す。
export const dayKey = (isoString: string) => {
	const d = new Date(isoString)
	return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

// formatDayLabel は日付の区切り線に出す文字列を返す (「今日」「昨日」「11月24日 (月)」)。
export const formatDayLabel = (isoString: string, now: Date = new Date()) => {
	const d = new Date(isoString)
	const start = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
	const diffDays = Math.round((start(now) - start(d)) / 86400000)
	if (diffDays === 0) return '今日'
	if (diffDays === 1) return '昨日'
	const wd = ['日', '月', '火', '水', '木', '金', '土'][d.getDay()]
	const md = `${d.getMonth() + 1}月${d.getDate()}日 (${wd})`
	return d.getFullYear() === now.getFullYear() ? md : `${d.getFullYear()}年${md}`
}
