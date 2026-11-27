import { afterEach, describe, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Badge } from './Badge'
import { Button, IconButton } from './Button'
import { ListItem, ListSection } from './List'
import { Modal } from './Modal'
import { Page } from './Page'
import { Segmented } from './Segmented'
import { SearchField } from './Fields'
import { Skeleton } from './Skeleton'
import { LoadingState } from './Spinner'
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

	test('IconButton は読み込み中なら無効化されスピナーを出す', () => {
		const onClick = vi.fn()
		render(<IconButton label="送信" loading icon={<span>icon</span>} onClick={onClick} />)
		const btn = screen.getByRole('button', { name: '送信' })
		fireEvent.click(btn)
		expect(onClick).not.toHaveBeenCalled()
		expect(btn.getAttribute('aria-busy')).toBe('true')
		expect(screen.queryByText('icon')).toBeNull()
		expect(screen.getByRole('status')).toBeTruthy()
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

	test('Toggle は loading 中は操作できず確認中の状態を示す', () => {
		const onChange = vi.fn()
		render(<Toggle label="通知" checked={false} loading onChange={onChange} />)
		const sw = screen.getByRole('switch', { name: '通知' })
		fireEvent.click(sw)
		expect(onChange).not.toHaveBeenCalled()
		expect(sw.getAttribute('aria-busy')).toBe('true')
		expect(sw.getAttribute('data-loading')).toBe('true')
		expect(sw.hasAttribute('disabled')).toBe(true)
	})

	test('ListSection と ListItem の loading でスピナーとプレースホルダーが出る', () => {
		const onClick = vi.fn()
		const { container } = render(
			<ListSection title="検索結果" loading>
				<ListItem title="状態" end="有効" loading />
				<ListItem loading />
				<ListItem title="テスト送信" loading onClick={onClick} />
			</ListSection>
		)
		expect(container.querySelectorAll('.nui-spinner').length).toBe(2)
		expect(screen.queryByText('有効')).toBeNull()
		expect(container.querySelectorAll('.nui-skeleton').length).toBe(2)
		fireEvent.click(screen.getByText('テスト送信'))
		expect(onClick).not.toHaveBeenCalled()
	})

	test('LoadingState, Skeleton, Page の loading 表示が正しく動く', () => {
		const { container, rerender } = render(<LoadingState message="取得中…" />)
		expect(screen.getByText('取得中…')).toBeTruthy()
		expect(container.querySelectorAll('.nui-spinner').length).toBe(1)

		rerender(
			<Page title="通知" loading loadingMessage="読み込み中…">
				<p>本文</p>
			</Page>
		)
		expect(screen.queryByText('本文')).toBeNull()
		expect(screen.getByText('読み込み中…')).toBeTruthy()

		rerender(<Skeleton width={80} height={16} variant="circle" />)
		expect(container.querySelector('.nui-skeleton')?.getAttribute('data-variant')).toBe('circle')
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
