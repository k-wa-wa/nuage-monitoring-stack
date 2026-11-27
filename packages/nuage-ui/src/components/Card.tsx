import type { HTMLAttributes } from 'react'

/** カードである (状態とアクションの最小単位)。 */
export function Card({ className, ...rest }: HTMLAttributes<HTMLDivElement>) {
	return <div className={`nui-card ${className ?? ''}`.trim()} {...rest} />
}
