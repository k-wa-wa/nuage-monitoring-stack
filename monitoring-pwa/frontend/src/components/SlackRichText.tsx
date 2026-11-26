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

function renderTextObject(
	obj: SlackTextObject,
	botUserId?: string,
	botName?: string,
	channelsById?: Record<string, string>
) {
	return (
		<GfmContent
			text={obj.text || ''}
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
		? blocks.filter((b) => b.type === 'section' || b.type === 'context' || b.type === 'divider')
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
					return (
						<div key={idx} className="slack-block-context">
							{elements.map((el, elIdx) => (
								<span key={elIdx} className="slack-context-item">
									{renderTextObject(el, botUserId, botName, channelsById)}
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
								{renderTextObject(block.text, botUserId, botName, channelsById)}
							</div>
						)}
						{Array.isArray(block.fields) && block.fields.length > 0 && (
							<div className="slack-section-fields">
								{block.fields.map((field, fIdx) => (
									<div key={fIdx} className="slack-section-field">
										{renderTextObject(field, botUserId, botName, channelsById)}
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
