import type { ReactNode } from 'react'
import { parseMrkdwn, parseMrkdwnInline, type MrkdwnInlineToken } from '../utils/mrkdwn'
import type { SlackBlock, SlackTextObject } from '../types/chat'

interface SlackRichTextProps {
	text: string
	blocks?: SlackBlock[]
	botUserId?: string
	botName?: string
}

function renderInlineTokens(
	tokens: MrkdwnInlineToken[],
	botUserId?: string,
	botName?: string
): ReactNode[] {
	return tokens.map((tok, idx) => {
		switch (tok.type) {
			case 'text':
				return <span key={idx}>{tok.text}</span>
			case 'bold':
				return <strong key={idx}>{renderInlineTokens(tok.children, botUserId, botName)}</strong>
			case 'italic':
				return <em key={idx}>{renderInlineTokens(tok.children, botUserId, botName)}</em>
			case 'strike':
				return <del key={idx}>{renderInlineTokens(tok.children, botUserId, botName)}</del>
			case 'code':
				return (
					<code key={idx} className="slack-inline-code">
						{tok.text}
					</code>
				)
			case 'link':
				return (
					<a
						key={idx}
						href={tok.url}
						target="_blank"
						rel="noopener noreferrer"
						className="slack-link"
					>
						{tok.label}
					</a>
				)
			case 'mention': {
				const displayName =
					botUserId && tok.userId === botUserId ? botName || 'nuage-bot' : tok.userId
				return (
					<span key={idx} className="slack-mention">
						@{displayName}
					</span>
				)
			}
		}
	})
}

function MrkdwnBlockText({
	text,
	plainOnly,
	botUserId,
	botName
}: {
	text: string
	plainOnly?: boolean
	botUserId?: string
	botName?: string
}) {
	if (!text) return null
	if (plainOnly) {
		return <span className="slack-plain-text">{text}</span>
	}
	const blocks = parseMrkdwn(text)
	return (
		<div className="slack-mrkdwn">
			{blocks.map((blk, idx) => {
				if (blk.type === 'codeblock') {
					return (
						<pre key={idx} className="slack-codeblock">
							<code>{blk.code}</code>
						</pre>
					)
				}
				if (blk.type === 'quote') {
					return (
						<blockquote key={idx} className="slack-quote">
							{renderInlineTokens(blk.tokens, botUserId, botName)}
						</blockquote>
					)
				}
				if (blk.tokens.length === 0) {
					return <div key={idx} className="slack-empty-line" />
				}
				return (
					<p key={idx} className="slack-paragraph">
						{renderInlineTokens(blk.tokens, botUserId, botName)}
					</p>
				)
			})}
		</div>
	)
}

function renderTextObject(
	obj: SlackTextObject,
	botUserId?: string,
	botName?: string
): ReactNode {
	if (obj.type === 'plain_text') {
		return <span className="slack-plain-text">{obj.text}</span>
	}
	return (
		<MrkdwnBlockText
			text={obj.text}
			plainOnly={false}
			botUserId={botUserId}
			botName={botName}
		/>
	)
}

// SlackRichText は本文の mrkdwn および Block Kit (section / context / divider) を描画する。
export default function SlackRichText({ text, blocks, botUserId, botName }: SlackRichTextProps) {
	const supportedBlocks = Array.isArray(blocks)
		? blocks.filter((b) => b.type === 'section' || b.type === 'context' || b.type === 'divider')
		: []

	if (supportedBlocks.length === 0) {
		return <MrkdwnBlockText text={text} botUserId={botUserId} botName={botName} />
	}

	return (
		<div className="slack-blocks">
			{supportedBlocks.map((block, idx) => {
				if (block.type === 'divider') {
					return <hr key={idx} className="slack-divider" />
				}
				if (block.type === 'context') {
					const elements = Array.isArray(block.elements) ? block.elements : []
					return (
						<div key={idx} className="slack-block-context">
							{elements.map((el, elIdx) => (
								<span key={elIdx} className="slack-context-item">
									{el.type === 'plain_text'
										? el.text
										: renderInlineTokens(parseMrkdwnInline(el.text || ''), botUserId, botName)}
								</span>
							))}
						</div>
					)
				}
				// section
				return (
					<div key={idx} className="slack-block-section">
						{block.text && (
							<div className="slack-section-text">
								{renderTextObject(block.text, botUserId, botName)}
							</div>
						)}
						{Array.isArray(block.fields) && block.fields.length > 0 && (
							<div className="slack-section-fields">
								{block.fields.map((field, fIdx) => (
									<div key={fIdx} className="slack-section-field">
										{renderTextObject(field, botUserId, botName)}
									</div>
								))}
							</div>
						)}
					</div>
				)
			})}
		</div>
	)
}
