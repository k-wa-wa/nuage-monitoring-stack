import { Spinner } from './Spinner'

interface ToggleProps {
	checked: boolean
	onChange: (checked: boolean) => void
	label: string
	disabled?: boolean
	loading?: boolean
}

/** オン・オフのスイッチである。loading 中は操作不可にし、確認・切り替え中であることを示す。 */
export function Toggle({ checked, onChange, label, disabled, loading = false }: ToggleProps) {
	return (
		<button
			type="button"
			role="switch"
			aria-checked={checked}
			aria-label={label}
			aria-busy={loading || undefined}
			disabled={disabled || loading}
			className="nui-toggle nui-pressable"
			data-checked={checked ? 'true' : 'false'}
			data-loading={loading ? 'true' : 'false'}
			onClick={() => onChange(!checked)}
		>
			{loading ? <Spinner size={14} label={label} /> : <span className="nui-toggle-thumb" />}
		</button>
	)
}
