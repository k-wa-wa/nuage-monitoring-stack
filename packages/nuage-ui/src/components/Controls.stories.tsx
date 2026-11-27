import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { Bell, Send, Settings } from 'lucide-react'
import { Badge, Chip } from './Badge'
import { Button, IconButton } from './Button'
import { Card } from './Card'
import { SearchField, TextArea, TextField } from './Fields'
import { Segmented } from './Segmented'
import { EmptyState, Spinner } from './Spinner'
import { Toggle } from './Toggle'

const meta: Meta = { title: 'Components/Controls' }
export default meta

export const Buttons: StoryObj = {
	render: () => (
		<div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
			<Button variant="primary">主アクション</Button>
			<Button>標準</Button>
			<Button variant="ghost">控えめ</Button>
			<Button variant="danger">ログアウト</Button>
			<Button size="sm">小</Button>
			<Button loading>送信中</Button>
			<Button disabled>押せない</Button>
			<IconButton label="設定" icon={<Settings size={18} />} />
			<IconButton label="送信" variant="filled" icon={<Send size={16} />} />
		</div>
	)
}

export const BadgesAndChips: StoryObj = {
	render: () => (
		<div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
			<Badge count={3} />
			<Badge count={120} />
			<Chip>neutral</Chip>
			<Chip tone="accent">APP</Chip>
			<Chip tone="warn">発生中</Chip>
			<Chip tone="success">解決済み</Chip>
			<Chip tone="blocked">保留</Chip>
		</div>
	)
}

export const Cards: StoryObj = {
	render: () => (
		<Card>
			<strong>KubeJobFailed</strong>
			<p style={{ color: 'var(--muted)' }}>Job failed to complete.</p>
		</Card>
	)
}

export const Fields: StoryObj = {
	render: function Render() {
		const [q, setQ] = useState('')
		const [text, setText] = useState('')
		return (
			<div style={{ display: 'grid', gap: 12, maxWidth: 420 }}>
				<SearchField value={q} onChange={setQ} />
				<TextField label="合言葉" type="password" placeholder="合言葉を入力" />
				<TextField label="エラーあり" error="合言葉が正しくない。" defaultValue="wrong" />
				<TextArea placeholder="# general へのメッセージ" value={text} onChange={(e) => setText(e.target.value)} />
			</div>
		)
	}
}

export const SegmentedAndToggle: StoryObj = {
	render: function Render() {
		const [v, setV] = useState<'all' | 'firing' | 'resolved'>('all')
		const [on, setOn] = useState(true)
		return (
			<div style={{ display: 'grid', gap: 12, maxWidth: 420 }}>
				<Segmented
					label="絞り込み"
					value={v}
					onChange={setV}
					options={[
						{ value: 'all', label: 'すべて' },
						{ value: 'firing', label: '発生中 2' },
						{ value: 'resolved', label: '解決済み' }
					]}
				/>
				<label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
					<Toggle label="プッシュ通知" checked={on} onChange={setOn} /> プッシュ通知
				</label>
			</div>
		)
	}
}

export const LoadingAndEmpty: StoryObj = {
	render: () => (
		<div>
			<Spinner />
			<EmptyState>
				<Bell size={20} /> まだ通知はない。
			</EmptyState>
		</div>
	)
}
