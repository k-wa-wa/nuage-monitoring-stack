import { forwardRef, useLayoutEffect, useRef, type InputHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { Search, X } from 'lucide-react'

interface TextFieldProps extends InputHTMLAttributes<HTMLInputElement> {
	label?: string
	error?: string | null
}

/** 1 行の入力欄である。スマホでは 16px 以上 (iOS の拡大を防ぐ)。 */
export const TextField = forwardRef<HTMLInputElement, TextFieldProps>(function TextField(
	{ label, error, id, className, ...rest },
	ref
) {
	const inputId = id ?? (label ? `nui-field-${label}` : undefined)
	return (
		<div className={`nui-field ${className ?? ''}`.trim()} data-invalid={error ? 'true' : 'false'}>
			{label ? (
				<label className="nui-field-label" htmlFor={inputId}>
					{label}
				</label>
			) : null}
			<input ref={ref} id={inputId} className="nui-input" aria-invalid={error ? true : undefined} {...rest} />
			{error ? (
				<p className="nui-field-error" role="alert">
					{error}
				</p>
			) : null}
		</div>
	)
})

interface TextAreaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
	/** 入力に合わせて伸びる最大の行数 */
	maxRows?: number
}

/** 入力に合わせて高さが伸びる、複数行の入力欄である。 */
export const TextArea = forwardRef<HTMLTextAreaElement, TextAreaProps>(function TextArea(
	{ maxRows = 6, value, className, rows = 1, ...rest },
	ref
) {
	const innerRef = useRef<HTMLTextAreaElement | null>(null)
	useLayoutEffect(() => {
		const el = innerRef.current
		if (!el) return
		el.style.height = 'auto'
		const style = window.getComputedStyle(el)
		const line = parseFloat(style.lineHeight) || 20
		const pad = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom)
		const max = line * maxRows + pad
		el.style.height = `${Math.min(el.scrollHeight, max)}px`
		el.style.overflowY = el.scrollHeight > max ? 'auto' : 'hidden'
	}, [value, maxRows])
	return (
		<textarea
			ref={(el) => {
				innerRef.current = el
				if (typeof ref === 'function') ref(el)
				else if (ref) ref.current = el
			}}
			rows={rows}
			value={value}
			className={`nui-input nui-textarea ${className ?? ''}`.trim()}
			{...rest}
		/>
	)
})

interface SearchFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'onChange' | 'value'> {
	value: string
	onChange: (value: string) => void
}

/** 検索欄である。中身があれば右に消すボタンを出す。 */
export function SearchField({ value, onChange, placeholder = '検索', className, ...rest }: SearchFieldProps) {
	return (
		<div className={`nui-search ${className ?? ''}`.trim()}>
			<Search size={16} className="nui-search-icon" aria-hidden="true" />
			<input
				type="search"
				className="nui-input nui-search-input"
				value={value}
				placeholder={placeholder}
				aria-label={placeholder}
				onChange={(e) => onChange(e.target.value)}
				{...rest}
			/>
			{value ? (
				<button type="button" className="nui-search-clear nui-pressable" aria-label="消す" onClick={() => onChange('')}>
					<X size={14} aria-hidden="true" />
				</button>
			) : null}
		</div>
	)
}
