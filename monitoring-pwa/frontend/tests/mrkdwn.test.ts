import { describe, expect, test } from 'bun:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import SlackRichText from '../src/components/SlackRichText'
import { preprocessSlackSyntax } from '../src/utils/mrkdwn'

function renderRichText(text: string, blocks?: Parameters<typeof SlackRichText>[0]['blocks']) {
	return renderToStaticMarkup(
		createElement(SlackRichText, {
			text,
			blocks,
			botUserId: 'U01BOT',
			botName: 'nuage-bot',
			channelsById: { C01ALERTMGR: 'alertmanager' }
		})
	)
}

describe('preprocessSlackSyntax', () => {
	test('山括弧記法を変換しつつコードブロック内はそのまま残す', () => {
		const out = preprocessSlackSyntax(
			'Hi <@U01BOT> in <#C01ALERTMGR> <!here> <https://monitoring.wpcapp.net|Grafana> `<@U01BOT>`',
			{
				botUserId: 'U01BOT',
				botName: 'nuage-bot',
				channelsById: { C01ALERTMGR: 'alertmanager' }
			}
		)
		expect(out).toContain('@nuage-bot')
		expect(out).toContain('#alertmanager')
		expect(out).toContain('@here')
		expect(out).toContain('[Grafana](https://monitoring.wpcapp.net/)')
		expect(out).toContain('`<@U01BOT>`')
	})
})

describe('SlackRichText (GFM + サニタイズ済み HTML)', () => {
	test('GFM の表・タスクリスト・打ち消し線・自動リンク・コードブロックを描画する', () => {
		const md = [
			'| Key | Val |',
			'| --- | --- |',
			'| A | 1 |',
			'',
			'- [x] 完了',
			'- [ ] 未完',
			'',
			'~~削除~~ https://example.com/auto',
			'',
			'```sh',
			'echo ok',
			'```'
		].join('\n')
		const html = renderRichText(md)
		expect(html).toContain('<table')
		expect(html).toContain('<th>Key</th>')
		expect(html).toContain('type="checkbox"')
		expect(html).toContain('<del>削除</del>')
		expect(html).toContain('href="https://example.com/auto"')
		expect(html).toContain('target="_blank"')
		expect(html).toContain('rel="noopener noreferrer"')
		expect(html).toContain('<pre class="slack-codeblock">')
	})

	test('mrkdwn を GFM に変換せず、GFM の意味 (*斜体*, **太字**) で描画する', () => {
		const html = renderRichText('*斜体* **太字** _下線斜体_')
		expect(html).toContain('<em>斜体</em>')
		expect(html).toContain('<strong>太字</strong>')
		expect(html).toContain('<em>下線斜体</em>')
	})

	test('Slack 固有記法 (<@U...>, <#C...>, <!here>, <!channel>, <url|label>) を描画する', () => {
		const html = renderRichText(
			'<@U01BOT> <#C01ALERTMGR> <!here> <!channel> <https://argocd.wpcapp.net|Argo CD>'
		)
		expect(html).toContain('@nuage-bot')
		expect(html).toContain('#alertmanager')
		expect(html).toContain('@here')
		expect(html).toContain('@channel')
		expect(html).toContain('href="https://argocd.wpcapp.net/"')
		expect(html).toContain('target="_blank"')
		expect(html).toContain('rel="noopener noreferrer"')
		expect(html).toContain('>Argo CD</a>')
	})

	test('安全な HTML は描画し、XSS ペイロードはすべて無害化する', () => {
		const xssInput = [
			'<b>安全太字</b>',
			'<script>alert("xss")</script>',
			'<style>body{display:none}</style>',
			'<iframe src="https://evil.example"></iframe>',
			'<object data="https://evil.example/a.swf"></object>',
			'<img src="https://example.com/a.png" onerror="alert(1)" />',
			'<svg onload="alert(1)"><circle /></svg>',
			'<a href="javascript:alert(1)" onclick="alert(2)">badlink</a>',
			'<javascript:alert(3)|evil>',
			'<div onmouseover="alert(4)">hover</div>'
		].join('\n')

		const html = renderRichText(xssInput)
		expect(html).toContain('<b>安全太字</b>')
		expect(html).not.toContain('<script')
		expect(html).not.toContain('alert(')
		expect(html).not.toContain('<style')
		expect(html).not.toContain('<iframe')
		expect(html).not.toContain('<object')
		expect(html).not.toContain('onerror')
		expect(html).not.toContain('onload')
		expect(html).not.toContain('onclick')
		expect(html).not.toContain('onmouseover')
		expect(html).not.toContain('javascript:')
		expect(html).not.toContain('<svg')
	})

	test('Block Kit の section と context も同じ GFM + サニタイズ描画を通る', () => {
		const html = renderRichText('fallback', [
			{
				type: 'section',
				text: {
					type: 'mrkdwn',
					text: '**見出し** <@U01BOT> <script>alert(1)</script>'
				},
				fields: [{ type: 'mrkdwn', text: '~~old~~ <i>italic</i>' }]
			},
			{
				type: 'context',
				elements: [{ type: 'mrkdwn', text: '<!channel> <img src=x onerror=alert(1)>' }]
			}
		])
		expect(html).toContain('<strong>見出し</strong>')
		expect(html).toContain('@nuage-bot')
		expect(html).toContain('<del>old</del>')
		expect(html).toContain('<i>italic</i>')
		expect(html).toContain('@channel')
		expect(html).not.toContain('<script')
		expect(html).not.toContain('onerror')
	})
})
