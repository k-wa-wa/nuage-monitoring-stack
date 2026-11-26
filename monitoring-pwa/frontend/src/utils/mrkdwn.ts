import { sanitizeExternalUrl } from './url'

export type MrkdwnInlineToken =
	| { type: 'text'; text: string }
	| { type: 'bold'; children: MrkdwnInlineToken[] }
	| { type: 'italic'; children: MrkdwnInlineToken[] }
	| { type: 'strike'; children: MrkdwnInlineToken[] }
	| { type: 'code'; text: string }
	| { type: 'link'; url: string; label: string }
	| { type: 'mention'; userId: string }

export type MrkdwnBlock =
	| { type: 'codeblock'; code: string }
	| { type: 'quote'; tokens: MrkdwnInlineToken[] }
	| { type: 'paragraph'; tokens: MrkdwnInlineToken[] }

// Slack mrkdwn のインライン構文を安全なトークン配列へパースする。
export function parseMrkdwnInline(input: string): MrkdwnInlineToken[] {
	const tokens: MrkdwnInlineToken[] = []
	let i = 0

	const pushText = (s: string) => {
		if (!s) return
		const last = tokens[tokens.length - 1]
		if (last && last.type === 'text') {
			last.text += s
		} else {
			tokens.push({ type: 'text', text: s })
		}
	}

	while (i < input.length) {
		// <url|label>, <url>, <@USER_ID>
		if (input[i] === '<') {
			const end = input.indexOf('>', i + 1)
			if (end !== -1) {
				const inner = input.slice(i + 1, end)
				if (inner.startsWith('@') && inner.length > 1) {
					tokens.push({ type: 'mention', userId: inner.slice(1) })
					i = end + 1
					continue
				}
				const pipeIdx = inner.indexOf('|')
				const rawUrl = pipeIdx !== -1 ? inner.slice(0, pipeIdx) : inner
				const label = pipeIdx !== -1 ? inner.slice(pipeIdx + 1) : inner
				const safeUrl = sanitizeExternalUrl(rawUrl)
				if (safeUrl) {
					tokens.push({ type: 'link', url: safeUrl, label: label || safeUrl })
					i = end + 1
					continue
				}
			}
		}

		// インラインコード `...`
		if (input[i] === '`') {
			const end = input.indexOf('`', i + 1)
			if (end > i + 1) {
				tokens.push({ type: 'code', text: input.slice(i + 1, end) })
				i = end + 1
				continue
			}
		}

		// *bold*, _italic_, ~strike~
		const ch = input[i]
		if (ch === '*' || ch === '_' || ch === '~') {
			const prevOk = i === 0 || /\s|[({[]/.test(input[i - 1])
			if (prevOk) {
				const end = input.indexOf(ch, i + 1)
				if (end > i + 1) {
					const inner = input.slice(i + 1, end)
					if (!inner.includes('\n')) {
						const childTokens = parseMrkdwnInline(inner)
						if (ch === '*') {
							tokens.push({ type: 'bold', children: childTokens })
						} else if (ch === '_') {
							tokens.push({ type: 'italic', children: childTokens })
						} else {
							tokens.push({ type: 'strike', children: childTokens })
						}
						i = end + 1
						continue
					}
				}
			}
		}

		// 素の URL (https://... または http://...)
		if (input.startsWith('https://', i) || input.startsWith('http://', i)) {
			const match = input.slice(i).match(/^https?:\/\/[^\s<>]+/)
			if (match) {
				const safeUrl = sanitizeExternalUrl(match[0])
				if (safeUrl) {
					tokens.push({ type: 'link', url: safeUrl, label: match[0] })
					i += match[0].length
					continue
				}
			}
		}

		pushText(input[i])
		i++
	}

	return tokens
}

// Slack mrkdwn 本文をブロック単位（コードブロック・引用・段落）に分解する。
export function parseMrkdwn(input: string): MrkdwnBlock[] {
	if (!input) return []
	const blocks: MrkdwnBlock[] = []
	const segments = input.split(/```/)

	for (let idx = 0; idx < segments.length; idx++) {
		const seg = segments[idx]
		if (idx % 2 === 1) {
			blocks.push({ type: 'codeblock', code: seg.replace(/^\n+|\n+$/g, '') })
			continue
		}
		const trimmedSeg = seg.replace(/^\n+|\n+$/g, '')
		if (!trimmedSeg) continue

		const lines = trimmedSeg.split('\n')
		for (const line of lines) {
			if (line.startsWith('> ')) {
				blocks.push({ type: 'quote', tokens: parseMrkdwnInline(line.slice(2)) })
			} else if (line.startsWith('&gt; ')) {
				blocks.push({ type: 'quote', tokens: parseMrkdwnInline(line.slice(5)) })
			} else {
				blocks.push({ type: 'paragraph', tokens: parseMrkdwnInline(line) })
			}
		}
	}
	return blocks
}
