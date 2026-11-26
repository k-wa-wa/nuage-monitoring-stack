import { describe, expect, test } from 'bun:test'
import {
	GRAFANA_BASE_URL,
	extractExternalLinks,
	getLogsUrl,
	parseAppRoute,
	parseChatRoute,
	rewriteGeneratorUrl,
	sanitizeExternalUrl
} from '../src/utils/url'
import type { UnifiedNotification } from '../src/types/notification'

describe('sanitizeExternalUrl', () => {
	test('http と https のみ許可し、危険なスキームを拒否する', () => {
		expect(sanitizeExternalUrl('https://monitoring.wpcapp.net/grafana')).toBe(
			'https://monitoring.wpcapp.net/grafana'
		)
		expect(sanitizeExternalUrl('http://example.com/path')).toBe('http://example.com/path')
		expect(sanitizeExternalUrl('javascript:alert(1)')).toBeNull()
		expect(sanitizeExternalUrl('data:text/html,<script>')).toBeNull()
		expect(sanitizeExternalUrl('')).toBeNull()
	})
})

describe('rewriteGeneratorUrl', () => {
	test('クラスタ内部 URL を本番 Grafana URL に書き換える', () => {
		expect(
			rewriteGeneratorUrl('http://vm-grafana.nuage-monitoring-stack.svc.cluster.local/explore?left=abc')
		).toBe(`${GRAFANA_BASE_URL}/grafana/explore?left=abc`)

		expect(
			rewriteGeneratorUrl('http://vm-grafana.nuage-monitoring-stack.svc:80/explore?left=abc')
		).toBe(`${GRAFANA_BASE_URL}/grafana/explore?left=abc`)

		expect(rewriteGeneratorUrl('http://grafana/explore')).toBe(`${GRAFANA_BASE_URL}/grafana/explore`)
		expect(rewriteGeneratorUrl('/explore?left=xyz')).toBe(`${GRAFANA_BASE_URL}/grafana/explore?left=xyz`)
	})

	test('公開 URL はそのまま維持する', () => {
		expect(rewriteGeneratorUrl('https://grafana.wpcapp.net/d/node-health')).toBe(
			'https://grafana.wpcapp.net/d/node-health'
		)
	})
})

describe('extractExternalLinks', () => {
	test('一般通知では対象アプリリンクを抽出する', () => {
		const note: UnifiedNotification = {
			uid: 'notification-1',
			id: 1,
			kind: 'notification',
			source: 'argocd',
			title: 'Argo CD Synced',
			body: 'Synced',
			status: 'notice',
			level: 'info',
			url: 'https://argocd.wpcapp.net/applications/nuage',
			created_at: '2026-11-19T00:00:00Z',
			updated_at: '2026-11-19T00:00:00Z',
			timestamp: '2026-11-19T00:00:00Z'
		}
		const links = extractExternalLinks(note)
		expect(links).toEqual([
			{
				label: '対象アプリを開く',
				url: 'https://argocd.wpcapp.net/applications/nuage',
				variant: 'primary'
			}
		])
	})

	test('アラートではログ・Grafana・Runbook リンクを抽出する', () => {
		const alert: UnifiedNotification = {
			uid: 'alert-2',
			id: 2,
			kind: 'alert',
			source: 'alertmanager',
			title: 'PodCrashLooping',
			body: 'Pod is crash looping',
			status: 'firing',
			level: 'error',
			details: JSON.stringify({
				labels: { alertname: 'PodCrashLooping', namespace: 'default', pod: 'api-1' },
				annotations: { runbook_url: 'https://monitoring.wpcapp.net/runbook/pod' },
				generatorURL: 'http://vm-grafana.nuage-monitoring-stack.svc/explore?left=1'
			}),
			created_at: '2026-11-19T00:00:00Z',
			updated_at: '2026-11-19T00:00:00Z',
			timestamp: '2026-11-19T00:00:00Z'
		}
		const links = extractExternalLinks(alert)
		expect(links.map((l) => l.label)).toEqual([
			'ログを確認 (Grafana)',
			'Grafanaで確認',
			'Runbookを開く'
		])
		expect(links[0].url).toBe(
			getLogsUrl({ alertname: 'PodCrashLooping', namespace: 'default', pod: 'api-1' }) || ''
		)
	})
})

describe('parseAppRoute', () => {
	test('通知詳細・設定・旧ルートを正しく判定する', () => {
		expect(parseAppRoute('/')).toEqual({
			tab: 'notifications',
			selectedUid: null,
			detailKind: null,
			detailId: null,
			legacyHistoryId: null
		})
		expect(parseAppRoute('/settings')).toEqual({
			tab: 'settings',
			selectedUid: null,
			detailKind: null,
			detailId: null,
			legacyHistoryId: null
		})
		expect(parseAppRoute('/alerts/12')).toEqual({
			tab: 'notifications',
			selectedUid: 'alert-12',
			detailKind: 'alert',
			detailId: 12,
			legacyHistoryId: null
		})
		expect(parseAppRoute('/notifications/5')).toEqual({
			tab: 'notifications',
			selectedUid: 'notification-5',
			detailKind: 'notification',
			detailId: 5,
			legacyHistoryId: null
		})
		expect(parseAppRoute('/history/7')).toEqual({
			tab: 'notifications',
			selectedUid: null,
			detailKind: null,
			detailId: null,
			legacyHistoryId: 7
		})
	})
})

describe('parseChatRoute', () => {
	test('チャンネル・スレッド・ディープリンク・設定ルートを判定する', () => {
		expect(parseChatRoute('/channels/C01ALERTMGR')).toEqual({
			view: 'chat',
			channelId: 'C01ALERTMGR',
			threadTs: null,
			deepLinkKind: null,
			deepLinkId: null
		})
		expect(parseChatRoute('/channels/C01ALERTMGR/threads/1700000000.000100')).toEqual({
			view: 'chat',
			channelId: 'C01ALERTMGR',
			threadTs: '1700000000.000100',
			deepLinkKind: null,
			deepLinkId: null
		})
		expect(parseChatRoute('/alerts/12')).toEqual({
			view: 'chat',
			channelId: null,
			threadTs: null,
			deepLinkKind: 'alert',
			deepLinkId: 12
		})
		expect(parseChatRoute('/notifications/5')).toEqual({
			view: 'chat',
			channelId: null,
			threadTs: null,
			deepLinkKind: 'notification',
			deepLinkId: 5
		})
		expect(parseChatRoute('/settings')).toEqual({
			view: 'settings',
			channelId: null,
			threadTs: null,
			deepLinkKind: null,
			deepLinkId: null
		})
	})
})
