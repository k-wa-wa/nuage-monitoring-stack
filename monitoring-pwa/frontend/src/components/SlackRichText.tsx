import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import rehypeSanitize from 'rehype-sanitize'
import { preprocessSlackSyntax, slackSanitizeSchema } from '../utils/mrkdwn'
import { sanitizeExternalUrl } from '../utils/url'
import type { SlackBlock, SlackTextObject } from '../types/chat'

interface SlackRichTextProps {
	text: string
	blocks?: SlackBlock[]
	botUserId?: string
	botName?: string
	channelsById?: Record<string, string>
}

const markdownComponents: Components = {
	a: ({ node: _node, href, children, ...rest }) => {
		const safeHttp = sanitizeExternalUrl(href)
		const safeMailto = href && href.startsWith('mailto:') ? href : null
		const safeHref = safeHttp || safeMailto
		if (!safeHref) {
			return <span>{children}</span>
		}
		return (
			<a
				{...rest}
				href={safeHref}
				target="_blank"
				rel="noopener noreferrer"
				className="slack-link"
			>
				{children}
			</a>
		)
	},
	code: ({ node: _node, className, children, ...rest }) => {
		const isBlock = Boolean(className) || String(children).includes('\n')
		return (
			<code
				{...rest}
				className={isBlock ? className : 'slack-inline-code'}
			>
				{children}
			</code>
		)
	},
	pre: ({ node: _node, children, ...rest }) => (
		<pre {...rest} className="slack-codeblock">
			{children}
		</pre>
	),
	blockquote: ({ node: _node, children, ...rest }) => (
		<blockquote {...rest} className="slack-quote">
			{children}
		</blockquote>
	),
	table: ({ node: _node, children, ...rest }) => (
		<div className="gfm-table-wrap">
			<table {...rest} className="gfm-table">
				{children}
			</table>
		</div>
	)
}

function GfmContent({
	text,
	plainOnly,
	botUserId,
	botName,
	channelsById
}: {
	text: string
	plainOnly?: boolean
	botUserId?: string
	botName?: string
	channelsById?: Record<string, string>
}) {
	if (!text) return null
	if (plainOnly) {
		return <span className="slack-plain-text">{text}</span>
	}

	const prepared = preprocessSlackSyntax(text, { botUserId, botName, channelsById })
	return (
		<div className="slack-mrkdwn gfm-body">
			<ReactMarkdown
				remarkPlugins={[remarkGfm]}
				rehypePlugins={[rehypeRaw, [rehypeSanitize, slackSanitizeSchema]]}
				components={markdownComponents}
			>
				{prepared}
			</ReactMarkdown>
		</div>
	)
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function renderTextObject(
	obj: unknown,
	botUserId?: string,
	botName?: string,
	channelsById?: Record<string, string>
) {
	if (!isRecord(obj) || typeof obj.text !== 'string') {
		return null
	}
	return (
		<GfmContent
			text={obj.text}
			plainOnly={obj.type === 'plain_text'}
			botUserId={botUserId}
			botName={botName}
			channelsById={channelsById}
		/>
	)
}

// SlackRichText は GFM + サニタイズ済み HTML + Slack 山括弧記法および Block Kit (section / context / divider) を描画する。
export default function SlackRichText({
	text,
	blocks,
	botUserId,
	botName,
	channelsById
}: SlackRichTextProps) {
	const supportedBlocks = Array.isArray(blocks)
		? blocks.filter(
				(b): b is SlackBlock =>
					isRecord(b) &&
					(b.type === 'section' || b.type === 'context' || b.type === 'divider')
			)
		: []

	if (supportedBlocks.length === 0) {
		return (
			<GfmContent
				text={text}
				botUserId={botUserId}
				botName={botName}
				channelsById={channelsById}
			/>
		)
	}

	return (
		<div className="slack-blocks">
			{supportedBlocks.map((block, idx) => {
				if (block.type === 'divider') {
					return <hr key={idx} className="slack-divider" />
				}
				if (block.type === 'context') {
					const elements = Array.isArray(block.elements) ? block.elements : []
					const renderedElements = elements
						.map((el, elIdx) => {
							const content = renderTextObject(el, botUserId, botName, channelsById)
							if (!content) return null
							return (
								<span key={elIdx} className="slack-context-item">
									{content}
								</span>
							)
						})
						.filter(Boolean)
					return (
						<div key={idx} className="slack-block-context">
							{renderedElements}
						</div>
					)
				}
				// section
				const renderedSectionText = renderTextObject(
					block.text as SlackTextObject | undefined,
					botUserId,
					botName,
					channelsById
				)
				const renderedFields = Array.isArray(block.fields)
					? block.fields
							.map((field, fIdx) => {
								const content = renderTextObject(field, botUserId, botName, channelsById)
								if (!content) return null
								return (
									<div key={fIdx} className="slack-section-field">
										{content}
									</div>
								)
							})
							.filter(Boolean)
					: []
				return (
					<div key={idx} className="slack-block-section">
						{renderedSectionText && (
							<div className="slack-section-text">{renderedSectionText}</div>
						)}
						{renderedFields.length > 0 && (
							<div className="slack-section-fields">{renderedFields}</div>
						)}
					</div>
				)
			})}
		</div>
	)
}
