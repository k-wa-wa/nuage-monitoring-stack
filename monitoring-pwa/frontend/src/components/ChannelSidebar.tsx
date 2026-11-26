import { Hash, Bot, Search, Settings as SettingsIcon, LogOut } from 'lucide-react'
import type { AuthUser, BotInfo, Channel } from '../types/chat'

interface ChannelSidebarProps {
	channels: Channel[]
	activeChannelId: string | null
	activeView: 'chat' | 'settings'
	user: AuthUser
	bot: BotInfo
	onSelectChannel: (channelId: string) => void
	onOpenSearch: () => void
	onOpenSettings: () => void
	onLogout: () => void
}

// 左ペインのチャンネル・DM 一覧サイドバーである。
export default function ChannelSidebar({
	channels,
	activeChannelId,
	activeView,
	user,
	bot,
	onSelectChannel,
	onOpenSearch,
	onOpenSettings,
	onLogout
}: ChannelSidebarProps) {
	const publicChannels = channels.filter((ch) => ch.is_channel)
	const dmChannels = channels.filter((ch) => ch.is_im)

	return (
		<aside className="chat-sidebar" aria-label="チャンネル一覧">
			<div className="sidebar-header">
				<div className="sidebar-workspace">
					<span className="app-logo" aria-hidden="true" />
					<div className="sidebar-workspace-info">
						<span className="sidebar-workspace-title">Nuage Notify</span>
						<span className={`sidebar-bot-status ${bot.online ? 'online' : 'offline'}`}>
							<span className={`connection-dot ${bot.online ? 'online' : ''}`} />
							{bot.name}: {bot.online ? 'オンライン' : 'オフライン'}
						</span>
					</div>
				</div>
				<button
					type="button"
					onClick={onOpenSearch}
					className="icon-btn"
					aria-label="キーワード検索"
					title="キーワード検索"
				>
					<Search size={16} />
				</button>
			</div>

			<div className="sidebar-scroll">
				<div className="sidebar-group">
					<div className="sidebar-group-label">チャンネル</div>
					<ul className="sidebar-list">
						{publicChannels.map((ch) => {
							const isActive = activeView === 'chat' && activeChannelId === ch.id
							return (
								<li key={ch.id}>
									<button
										type="button"
										onClick={() => onSelectChannel(ch.id)}
										className={`channel-item ${isActive ? 'active' : ''} ${ch.unread_count > 0 ? 'has-unread' : ''}`}
									>
										<span className="channel-item-left">
											<Hash size={15} className="channel-icon" />
											<span className="channel-name">{ch.name}</span>
										</span>
										{ch.unread_count > 0 && (
											<span className="unread-badge" aria-label={`未読 ${ch.unread_count}件`}>
												{ch.unread_count > 99 ? '99+' : ch.unread_count}
											</span>
										)}
									</button>
								</li>
							)
						})}
					</ul>
				</div>

				<div className="sidebar-group">
					<div className="sidebar-group-label">ダイレクトメッセージ</div>
					<ul className="sidebar-list">
						{dmChannels.map((ch) => {
							const isActive = activeView === 'chat' && activeChannelId === ch.id
							return (
								<li key={ch.id}>
									<button
										type="button"
										onClick={() => onSelectChannel(ch.id)}
										className={`channel-item ${isActive ? 'active' : ''} ${ch.unread_count > 0 ? 'has-unread' : ''}`}
									>
										<span className="channel-item-left">
											<span className="dm-avatar-wrap">
												<Bot size={15} className="channel-icon" />
												<span className={`dm-presence-dot ${bot.online ? 'online' : 'offline'}`} />
											</span>
											<span className="channel-name">{bot.name}</span>
											{!bot.online && <span className="dm-offline-tag">オフライン</span>}
										</span>
										{ch.unread_count > 0 && (
											<span className="unread-badge" aria-label={`未読 ${ch.unread_count}件`}>
												{ch.unread_count > 99 ? '99+' : ch.unread_count}
											</span>
										)}
									</button>
								</li>
							)
						})}
					</ul>
				</div>
			</div>

			<div className="sidebar-footer">
				<div className="sidebar-user-pill">
					<span className="user-avatar-badge">{user.display_name.slice(0, 1).toUpperCase()}</span>
					<span className="sidebar-user-name">{user.display_name}</span>
				</div>
				<div className="sidebar-footer-actions">
					<button
						type="button"
						onClick={onOpenSettings}
						className={`icon-btn ${activeView === 'settings' ? 'active' : ''}`}
						aria-label="設定"
						title="設定"
					>
						<SettingsIcon size={16} />
					</button>
					<button
						type="button"
						onClick={onLogout}
						className="icon-btn"
						aria-label="ログアウト"
						title="ログアウト"
					>
						<LogOut size={16} />
					</button>
				</div>
			</div>
		</aside>
	)
}
