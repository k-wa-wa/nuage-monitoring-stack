import { defaultSchema } from 'rehype-sanitize'
import type { Schema } from 'hast-util-sanitize'
import { sanitizeExternalUrl } from './url'

export interface SlackFormatOptions {
	botUserId?: string
	botName?: string
	channelsById?: Record<string, string>
}

function escapeHtml(raw: string): string {
	return raw
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;')
		.replace(/'/g, '&#39;')
}

function escapeMarkdownLinkLabel(raw: string): string {
	return raw.replace(/[[\]\\]/g, (ch) => `\\${ch}`)
}

function replaceSlackAngleTokensOutsideCode(
	segment: string,
	opts?: SlackFormatOptions
): string {
	return segment.replace(/<([^>\n]+)>/g, (fullMatch, inner: string) => {
		// メンション: <@U01BOT> または <@U01BOT|label>
		const mentionMatch = inner.match(/^@([A-Za-z0-9_-]+)(?:\|([^>]+))?$/)
		if (mentionMatch) {
			const uid = mentionMatch[1]
			const label = mentionMatch[2]
			const resolved =
				label ||
				(opts?.botUserId && uid === opts.botUserId ? opts.botName || 'nuage-bot' : uid)
			return `<span class="slack-mention">@${escapeHtml(resolved)}</span>`
		}

		// チャンネル: <#C01ALERTMGR> または <#C01ALERTMGR|alertmanager>
		const channelMatch = inner.match(/^#([A-Za-z0-9_-]+)(?:\|([^>]+))?$/)
		if (channelMatch) {
			const cid = channelMatch[1]
			const label = channelMatch[2] || opts?.channelsById?.[cid] || cid
			return `<span class="slack-channel">#${escapeHtml(label)}</span>`
		}

		// 特殊メンション: <!here>, <!channel>, <!everyone>
		const specialMatch = inner.match(/^!(here|channel|everyone)(?:\|([^>]+))?$/)
		if (specialMatch) {
			const label = specialMatch[2] || specialMatch[1]
			return `<span class="slack-mention slack-mention-special">@${escapeHtml(label)}</span>`
		}

		// Slack リンク: <https://...|label> または <https://...>
		const pipeIdx = inner.indexOf('|')
		if (pipeIdx !== -1) {
			const rawUrl = inner.slice(0, pipeIdx).trim()
			const rawLabel = inner.slice(pipeIdx + 1)
			const safeUrl = sanitizeExternalUrl(rawUrl)
			if (safeUrl) {
				return `[${escapeMarkdownLinkLabel(rawLabel)}](${safeUrl})`
			}
			// 危険スキーム (<javascript:...|click> 等) はリンク化せず平文ラベルに落とす
			if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(rawUrl)) {
				return escapeHtml(rawLabel)
			}
		} else {
			const trimmed = inner.trim()
			const safeUrl = sanitizeExternalUrl(trimmed)
			if (safeUrl) {
				return `[${escapeMarkdownLinkLabel(trimmed)}](${safeUrl})`
			}
			if (/^(?:javascript|data|vbscript):/i.test(trimmed)) {
				return ''
			}
		}

		// HTML タグ等はそのまま後段 (rehype-raw + rehype-sanitize) に渡す
		return fullMatch
	})
}

/**
 * Slack 固有の山括弧記法 (<@U…>, <#C…>, <!here>, <!channel>, <url|label>, <url>) を
 * Markdown / HTML として描画する前に安全な表現へ前処理する。
 * コードブロック・インラインコードの内部はそのまま保持する。
 */
export function preprocessSlackSyntax(input: string, opts?: SlackFormatOptions): string {
	if (!input) return ''

	// フェンスコードブロック (```...```) とインラインコード (`...`) を保護しながら置換する
	const codePattern = /(```[\s\S]*?```|`[^`\n]+`)/g
	let result = ''
	let lastIndex = 0
	let match: RegExpExecArray | null

	while ((match = codePattern.exec(input)) !== null) {
		if (match.index > lastIndex) {
			result += replaceSlackAngleTokensOutsideCode(input.slice(lastIndex, match.index), opts)
		}
		result += match[0]
		lastIndex = match.index + match[0].length
	}
	if (lastIndex < input.length) {
		result += replaceSlackAngleTokensOutsideCode(input.slice(lastIndex), opts)
	}
	return result
}

/**
 * GitHub 互換スキーマに基づく厳格な HTML サニタイズスキーマである。
 * script / style / iframe / object / on* 属性 / javascript: スキームを排除する。
 */
export const slackSanitizeSchema: Schema = {
	...defaultSchema,
	attributes: {
		...defaultSchema.attributes,
		a: [...(defaultSchema.attributes?.a || []), 'target', 'rel'],
		span: [
			...(defaultSchema.attributes?.span || []),
			['className', 'slack-mention', 'slack-mention-special', 'slack-channel']
		]
	},
	protocols: {
		...defaultSchema.protocols,
		href: ['http', 'https', 'mailto'],
		src: ['http', 'https']
	}
}
