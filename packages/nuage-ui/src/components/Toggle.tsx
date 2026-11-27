interface ToggleProps {
	checked: boolean
	onChange: (checked: boolean) => void
	label: string
	disabled?: boolean
}

/** オン・オフのスイッチである。 */
export function Toggle({ checked, onChange, label, disabled }: ToggleProps) {
	return (
		<button
			type="button"
			role="switch"
			aria-checked={checked}
			aria-label={label}
			disabled={disabled}
			className="nui-toggle nui-pressable"
			data-checked={checked ? 'true' : 'false'}
			onClick={() => onChange(!checked)}
		>
			<span className="nui-toggle-thumb" />
		</button>
	)
}
