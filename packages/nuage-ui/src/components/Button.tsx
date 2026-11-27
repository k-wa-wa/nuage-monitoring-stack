import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { Spinner } from './Spinner'

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
	variant?: Variant
	size?: 'sm' | 'md'
	block?: boolean
	loading?: boolean
	icon?: ReactNode
}

/** ボタンである。押した瞬間に沈み、離すと弾んで戻る (規約 2.5 のモーション)。 */
export function Button({
	variant = 'secondary',
	size = 'md',
	block = false,
	loading = false,
	icon,
	children,
	className,
	disabled,
	type = 'button',
	...rest
}: ButtonProps) {
	return (
		<button
			type={type}
			className={`nui-button nui-pressable ${className ?? ''}`.trim()}
			data-variant={variant}
			data-size={size}
			data-block={block ? 'true' : 'false'}
			disabled={disabled || loading}
			aria-busy={loading || undefined}
			{...rest}
		>
			{loading ? <Spinner size={14} /> : icon}
			{children ? <span>{children}</span> : null}
		</button>
	)
}

interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
	/** 読み上げ用の名前 (必須) */
	label: string
	icon: ReactNode
	variant?: 'ghost' | 'filled'
}

/** アイコンだけのボタンである。押した時の縮みはアイコン用の比率 (--scale-icon-active)。 */
export function IconButton({ label, icon, variant = 'ghost', className, type = 'button', ...rest }: IconButtonProps) {
	return (
		<button
			type={type}
			aria-label={label}
			title={label}
			className={`nui-icon-button nui-pressable ${className ?? ''}`.trim()}
			data-variant={variant}
			{...rest}
		>
			{icon}
		</button>
	)
}
