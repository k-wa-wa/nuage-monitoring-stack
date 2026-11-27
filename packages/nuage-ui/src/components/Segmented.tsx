import type { ReactNode } from 'react'

export interface SegmentOption<T extends string> {
	value: T
	label: ReactNode
}

interface SegmentedProps<T extends string> {
	options: SegmentOption<T>[]
	value: T
	onChange: (value: T) => void
	label: string
}

/** 切り替え (iOS の Segmented Control) である。 */
export function Segmented<T extends string>({ options, value, onChange, label }: SegmentedProps<T>) {
	return (
		<div className="nui-segmented" role="tablist" aria-label={label}>
			{options.map((o) => (
				<button
					key={o.value}
					type="button"
					role="tab"
					aria-selected={o.value === value}
					data-value={o.value}
					className="nui-segment nui-pressable"
					onClick={() => onChange(o.value)}
				>
					{o.label}
				</button>
			))}
		</div>
	)
}
