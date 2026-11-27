import { useMemo, useState, type ImgHTMLAttributes } from 'react'
import { createPortal } from 'react-dom'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import rehypeSanitize from 'rehype-sanitize'
import ImageLightbox from './ImageLightbox'
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

interface LightboxImage {
	src: string
	alt: string
}

function SlackInlineImage({
	src,
	alt,
	onOpen,
	...rest
}: {
	src: string
	alt: string
	onOpen: () => void
} & ImgHTMLAttributes<HTMLImageElement>) {
	const [loaded, setLoaded] = useState(false)
	const label = alt || '画像'
	return (
		<button
			type="button"
			onClick={onOpen}
			className="slack-image-trigger"
			data-loaded={loaded ? 'true' : 'false'}
			aria-label={`${label} を全画面で表示`}
		>
			<img
				{...rest}
				src={src}
				alt={alt}
				className="slack-inline-image"
				loading="lazy"
				onLoad={() => setLoaded(true)}
				onError={() => setLoaded(true)}
			/>
		</button>
	)
}

function createMarkdownComponents(onOpenImage: (img: LightboxImage) => void): Components {
	return {
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
		img: ({ node: _node, src, alt, ...rest }) => {
			const safeSrc = sanitizeExternalUrl(src)
			if (!safeSrc) {
				return null
			}
			const label = alt || '画像'
			return (
				<SlackInlineImage
					{...rest}
					src={safeSrc}
					alt={alt || ''}
					onOpen={() => onOpenImage({ src: safeSrc, alt: label })}
				/>
			)
		},
		code: ({ node: _node, className, children, ...rest }) => {
			const isBlock = Boolean(className) || String(children).includes('\n')
			return (
				<code {...rest} className={isBlock ? className : 'slack-inline-code'}>
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
}

function GfmContent({
	text,
	plainOnly,
	botUserId,
	botName,
	channelsById,
	components
}: {
	text: string
	plainOnly?: boolean
	botUserId?: string
	botName?: string
	channelsById?: Record<string, string>
	components: Components
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
				components={components}
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
	components: Components,
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
			components={components}
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
	const [lightbox, setLightbox] = useState<LightboxImage | null>(null)
	const markdownComponents = useMemo(() => createMarkdownComponents(setLightbox), [])

	const supportedBlocks = Array.isArray(blocks)
		? blocks.filter(
				(b): b is SlackBlock =>
					isRecord(b) &&
					(b.type === 'section' || b.type === 'context' || b.type === 'divider')
			)
		: []

	const content =
		supportedBlocks.length === 0 ? (
			<GfmContent
				text={text}
				botUserId={botUserId}
				botName={botName}
				channelsById={channelsById}
				components={markdownComponents}
			/>
		) : (
			<div className="slack-blocks">
				{supportedBlocks.map((block, idx) => {
					if (block.type === 'divider') {
						return <hr key={idx} className="slack-divider" />
					}
					if (block.type === 'context') {
						const elements = Array.isArray(block.elements) ? block.elements : []
						const renderedElements = elements
							.map((el, elIdx) => {
								const rendered = renderTextObject(
									el,
									markdownComponents,
									botUserId,
									botName,
									channelsById
								)
								if (!rendered) return null
								return (
									<span key={elIdx} className="slack-context-item">
										{rendered}
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
						markdownComponents,
						botUserId,
						botName,
						channelsById
					)
					const renderedFields = Array.isArray(block.fields)
						? block.fields
								.map((field, fIdx) => {
									const rendered = renderTextObject(
										field,
										markdownComponents,
										botUserId,
										botName,
										channelsById
									)
									if (!rendered) return null
									return (
										<div key={fIdx} className="slack-section-field">
											{rendered}
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

	return (
		<>
			{content}
			{lightbox &&
				typeof document !== 'undefined' &&
				createPortal(
					<ImageLightbox
						src={lightbox.src}
						alt={lightbox.alt}
						onClose={() => setLightbox(null)}
					/>,
					document.body
				)}
		</>
	)
}
