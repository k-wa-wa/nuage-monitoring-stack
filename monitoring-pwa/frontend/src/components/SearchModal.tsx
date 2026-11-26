import { useState, type FormEvent } from 'react'
import { Hash, Bot, Search, X } from 'lucide-react'
import { formatHistoryTimestamp } from '../utils/date'
import type { SearchResult } from '../types/chat'

interface SearchModalProps {
	apiBase: string
	onClose: () => void
	onSelectResult: (res: SearchResult) => void
}

// チャンネル横断のキーワード検索モーダルである。
export default function SearchModal({ apiBase, onClose, onSelectResult }: SearchModalProps) {
	const [query, setQuery] = useState('')
	const [results, setResults] = useState<SearchResult[]>([])
	const [searching, setSearching] = useState(false)
	const [searched, setSearched] = useState(false)

	const handleSearch = async (e: FormEvent) => {
		e.preventDefault()
		const q = query.trim()
		if (!q) return

		setSearching(true)
		setSearched(true)
		try {
			const res = await fetch(`${apiBase}/api/search?q=${encodeURIComponent(q)}`, {
				credentials: 'include'
			})
			if (res.ok) {
				const data: SearchResult[] = await res.json()
				setResults(data)
			}
		} catch (err) {
			console.error('Search failed:', err)
		} finally {
			setSearching(false)
		}
	}

	return (
		<div className="modal-backdrop" role="dialog" aria-modal="true" aria-label="メッセージ検索">
			<div className="search-modal">
				<div className="search-modal-header">
					<form onSubmit={handleSearch} className="search-form">
						<Search size={16} className="search-icon" />
						<input
							type="search"
							value={query}
							onChange={(e) => setQuery(e.target.value)}
							placeholder="アラート名・本文・詳細を検索..."
							autoFocus
							className="search-input"
						/>
						<button type="submit" disabled={searching || !query.trim()} className="btn btn-primary btn-sm">
							{searching ? '検索中' : '検索'}
						</button>
					</form>
					<button type="button" onClick={onClose} className="icon-btn" aria-label="閉じる">
						<X size={18} />
					</button>
				</div>

				<div className="search-results">
					{!searched ? (
						<div className="search-hint">キーワードを入力して履歴を検索する。</div>
					) : results.length === 0 && !searching ? (
						<div className="search-hint">一致するメッセージは見つからなかった。</div>
					) : (
						results.map((item) => (
							<button
								key={`${item.channel_id}-${item.message.ts}`}
								type="button"
								onClick={() => onSelectResult(item)}
								className="search-result-item"
							>
								<div className="search-result-meta">
									<span className="search-result-channel">
										{item.is_im ? <Bot size={12} /> : <Hash size={12} />}
										{item.channel_name}
									</span>
									<span className="search-result-author">{item.message.username}</span>
									<span className="search-result-time">
										{formatHistoryTimestamp(item.message.created_at)}
									</span>
								</div>
								<p className="search-result-text">{item.message.text}</p>
							</button>
						))
					)}
				</div>
			</div>
		</div>
	)
}
