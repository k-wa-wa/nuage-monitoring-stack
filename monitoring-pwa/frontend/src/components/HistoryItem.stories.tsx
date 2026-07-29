import { useState, type ComponentProps } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import HistoryItem from './HistoryItem'

const meta = {
	title: 'History/HistoryItem',
	component: HistoryItem,
	parameters: {
		layout: 'padded'
	},
	argTypes: {
		onToggle: { action: 'toggled' }
	}
} satisfies Meta<typeof HistoryItem>

export default meta
type Story = StoryObj<typeof meta>

const baseItem = {
	id: 1,
	title: 'PodCrashLoopBackOff',
	body: 'nuage-monitoring-stack/api-server-6d8f9c-xyz が CrashLoopBackOff になった。',
	level: 'error' as const,
	created_at: new Date().toISOString()
}

// タイムスタンプが「日付(MM/DD) + 時刻(HH:mm)」で表示されることを確認するストーリーである。
export const Error: Story = {
	args: {
		item: baseItem,
		isExpanded: false,
		onToggle: () => {},
		grafanaBase: 'https://monitoring.cluster.wpc'
	}
}

export const Warning: Story = {
	args: {
		...Error.args,
		item: { ...baseItem, id: 2, title: 'DiskSpaceLow', level: 'warning', body: 'node-worker-2 のディスク使用率が85%を超えた。' }
	}
}

export const Success: Story = {
	args: {
		...Error.args,
		item: { ...baseItem, id: 3, title: 'DeploySucceeded', level: 'success', body: 'monitoring-pwa のデプロイが正常に完了した。' }
	}
}

export const Info: Story = {
	args: {
		...Error.args,
		item: { ...baseItem, id: 4, title: 'ScheduledMaintenance', level: 'info', body: '来週火曜日にメンテナンスを予定している。' }
	}
}

// details を持つ通知を展開した状態のストーリーである（HistoryDetailPanel を含む）。
export const ExpandedWithDetails: Story = {
	args: {
		item: {
			...baseItem,
			details: JSON.stringify({
				labels: { alertname: 'PodCrashLoopBackOff', namespace: 'nuage-monitoring-stack', pod: 'api-server-6d8f9c-xyz' },
				annotations: { summary: 'Pod is crash looping', description: '過去10分で5回再起動した。' },
				generatorURL: 'https://prometheus.cluster.wpc/graph?g0.expr=up'
			})
		},
		isExpanded: true,
		onToggle: () => {},
		grafanaBase: 'https://monitoring.cluster.wpc'
	}
}

// クリックで開閉できる、実際のトグル挙動を確認するためのインタラクティブなストーリーである。
function InteractiveHistoryItem(args: ComponentProps<typeof HistoryItem>) {
	const [expanded, setExpanded] = useState(false)
	return <HistoryItem {...args} isExpanded={expanded} onToggle={() => setExpanded((v) => !v)} />
}

export const Interactive: Story = {
	render: (args) => <InteractiveHistoryItem {...args} />,
	args: {
		item: {
			...baseItem,
			id: 5,
			details: JSON.stringify({ labels: { alertname: 'PodCrashLoopBackOff' } })
		},
		isExpanded: false,
		onToggle: () => {},
		grafanaBase: 'https://monitoring.cluster.wpc'
	}
}
