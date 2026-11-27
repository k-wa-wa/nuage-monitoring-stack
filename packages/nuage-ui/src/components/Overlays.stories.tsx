import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button } from './Button'
import { Modal } from './Modal'
import { ToastProvider, useToast } from './Toast'

const meta: Meta = { title: 'Components/Overlays' }
export default meta

export const ModalAndSheet: StoryObj = {
	render: function Render() {
		const [open, setOpen] = useState(true)
		return (
			<>
				<Button onClick={() => setOpen(true)}>開く</Button>
				<Modal
					open={open}
					onClose={() => setOpen(false)}
					title="ログアウトする？"
					footer={
						<>
							<Button onClick={() => setOpen(false)}>キャンセル</Button>
							<Button variant="danger" onClick={() => setOpen(false)}>
								ログアウト
							</Button>
						</>
					}
				>
					<p>スマホ幅では下から出るシートになる。</p>
				</Modal>
			</>
		)
	}
}

function ToastDemo() {
	const show = useToast()
	return (
		<div style={{ display: 'flex', gap: 8 }}>
			<Button onClick={() => show('コピーした')}>標準</Button>
			<Button onClick={() => show('通知を受け取るように登録した。', 'success')}>成功</Button>
			<Button onClick={() => show('送信に失敗した。', 'warn')}>警告</Button>
		</div>
	)
}

export const Toasts: StoryObj = {
	render: () => (
		<ToastProvider>
			<ToastDemo />
		</ToastProvider>
	)
}
