import { describe, expect, test } from 'bun:test'
import { parseMrkdwn, parseMrkdwnInline } from '../src/utils/mrkdwn'

describe('parseMrkdwnInline', () => {
	test('bold / italic / strike / code / mention / link をパースする', () => {
		const tokens = parseMrkdwnInline(
			'こんにちは <@U01BOT> *太字* _斜体_ ~取消~ `cmd` <https://monitoring.wpcapp.net|Grafana>'
		)
		expect(tokens.some((t) => t.type === 'mention' && t.userId === 'U01BOT')).toBe(true)
		expect(tokens.some((t) => t.type === 'bold')).toBe(true)
		expect(tokens.some((t) => t.type === 'italic')).toBe(true)
		expect(tokens.some((t) => t.type === 'strike')).toBe(true)
		expect(tokens.some((t) => t.type === 'code' && t.text === 'cmd')).toBe(true)
		expect(
			tokens.some(
				(t) =>
					t.type === 'link' &&
					t.url === 'https://monitoring.wpcapp.net/' &&
					t.label === 'Grafana'
			)
		).toBe(true)
	})

	test('危険なURLスキームをリンク化しない', () => {
		const tokens = parseMrkdwnInline('<javascript:alert(1)|XSS>')
		expect(tokens.some((t) => t.type === 'link')).toBe(false)
	})
})

describe('parseMrkdwn', () => {
	test('コードブロックと引用と段落を分解する', () => {
		const blocks = parseMrkdwn('> 引用行\n```\nline1\nline2\n```\n通常行')
		expect(blocks[0].type).toBe('quote')
		expect(blocks[1]).toEqual({ type: 'codeblock', code: 'line1\nline2' })
		expect(blocks[2].type).toBe('paragraph')
	})
})
