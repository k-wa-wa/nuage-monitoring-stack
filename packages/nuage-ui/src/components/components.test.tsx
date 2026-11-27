import { afterEach, describe, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Badge } from './Badge'
import { Button } from './Button'
import { ListItem, ListSection } from './List'
import { Modal } from './Modal'
import { Segmented } from './Segmented'
import { SearchField } from './Fields'
import { Toggle } from './Toggle'

afterEach(cleanup)

describe('部品', () => {
	test('Badge は 0 以下なら出さず、99 を超えたら 99+ にする', () => {
		const { container, rerender } = render(<Badge count={0} />)
		expect(container.textContent).toBe('')
		rerender(<Badge count={120} />)
		expect(screen.getByText('99+')).toBeTruthy()
	})

	test('Button は読み込み中なら押せない', () => {
		const onClick = vi.fn()
		render(
			<Button loading onClick={onClick}>
				送る
			</Button>
		)
		fireEvent.click(screen.getByRole('button'))
		expect(onClick).not.toHaveBeenCalled()
		expect(screen.getByRole('button').getAttribute('aria-busy')).toBe('true')
	})

	test('ListItem は to があればリンク、onClick があればボタンになる', () => {
		const onClick = vi.fn()
		render(
			<MemoryRouter>
				<ListSection title="チャンネル">
					<ListItem title="general" to="/channels/C1" chevron />
					<ListItem title="設定" onClick={onClick} />
				</ListSection>
			</MemoryRouter>
		)
		expect(screen.getByText('general').closest('a')?.getAttribute('href')).toBe('/channels/C1')
		fireEvent.click(screen.getByText('設定'))
		expect(onClick).toHaveBeenCalledOnce()
	})

	test('Segmented は選んだ値を知らせ、選択中を aria-selected で示す', () => {
		const onChange = vi.fn()
		render(
			<Segmented
				label="絞り込み"
				value="all"
				onChange={onChange}
				options={[
					{ value: 'all', label: 'すべて' },
					{ value: 'firing', label: '発生中' }
				]}
			/>
		)
		expect(screen.getByRole('tab', { name: 'すべて' }).getAttribute('aria-selected')).toBe('true')
		fireEvent.click(screen.getByRole('tab', { name: '発生中' }))
		expect(onChange).toHaveBeenCalledWith('firing')
	})

	test('Toggle は押すと反対の値を知らせる', () => {
		const onChange = vi.fn()
		render(<Toggle label="通知" checked={false} onChange={onChange} />)
		fireEvent.click(screen.getByRole('switch', { name: '通知' }))
		expect(onChange).toHaveBeenCalledWith(true)
	})

	test('SearchField は中身があれば消すボタンを出す', () => {
		const onChange = vi.fn()
		const { rerender } = render(<SearchField value="" onChange={onChange} />)
		expect(screen.queryByRole('button', { name: '消す' })).toBeNull()
		rerender(<SearchField value="abc" onChange={onChange} />)
		fireEvent.click(screen.getByRole('button', { name: '消す' }))
		expect(onChange).toHaveBeenCalledWith('')
	})

	test('Modal は背景・× ・Esc で閉じ、中身を押しても閉じない', () => {
		const onClose = vi.fn()
		render(
			<Modal open title="詳細" onClose={onClose}>
				<p>中身</p>
			</Modal>
		)
		fireEvent.click(screen.getByText('中身'))
		expect(onClose).not.toHaveBeenCalled()
		fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
		fireEvent.keyDown(window, { key: 'Escape' })
		fireEvent.click(document.querySelector('.nui-modal-backdrop')!)
		expect(onClose).toHaveBeenCalledTimes(3)
	})
})
