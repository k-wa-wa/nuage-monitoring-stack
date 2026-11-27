import { expect, test, type Locator, type Page } from '@playwright/test'

// テスト用 240x220 SVG (外部通信を避けつつ、遅延読込で高さが伸びる状況を再現する)
const MOCK_SVG = Buffer.from(
	'<svg xmlns="http://www.w3.org/2000/svg" width="240" height="220" viewBox="0 0 240 220"><rect width="240" height="220" fill="#2c2a26"/><circle cx="120" cy="110" r="60" fill="#e07a5f"/></svg>',
	'utf-8'
)

async function setupRoutes(page: Page) {
	await page.route('https://mock-assets.local/**', async (route) => {
		await new Promise((resolve) => setTimeout(resolve, 120))
		await route.fulfill({ status: 200, contentType: 'image/svg+xml', body: MOCK_SVG })
	})
}

// 画面の移り変わりの動き (Web Animations) が終わるまで待つ (スピナー等の無限ループは除く)
async function settle(page: Page) {
	await page.waitForFunction(() =>
		document
			.getAnimations()
			.filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
			.every((a) => a.playState !== 'running')
	)
	await page.waitForTimeout(50)
}

async function login(page: Page) {
	await page.goto('/')
	const input = page.locator('#passphrase-input')
	await expect(input).toBeVisible()
	await input.fill('nuage-preview')
	await page.getByRole('button', { name: 'ログイン' }).click()
	await expect(page.locator('.nui-tabbar')).toBeVisible()
}

function channelItem(page: Page, name: string): Locator {
	return page.locator('.channel-item').filter({ hasText: name }).filter({ visible: true }).first()
}

function chatScroller(page: Page): Locator {
	return page.locator('.chat-page .nui-page-body').filter({ visible: true }).last()
}

function backButton(page: Page): Locator {
	return page.locator('.nui-back-button').filter({ visible: true }).last()
}

async function scrollMetrics(scroller: Locator) {
	return scroller.evaluate((el) => ({
		top: el.scrollTop,
		distanceToBottom: el.scrollHeight - el.scrollTop - el.clientHeight
	}))
}

async function scrollTo(scroller: Locator, top: number) {
	await scroller.evaluate((el, y) => {
		el.scrollTop = y
		el.dispatchEvent(new Event('scroll'))
	}, top)
}

async function openGeneral(page: Page) {
	await channelItem(page, 'general').click()
	await expect(page.locator('#chat-msg-1700000080\\.000100')).toBeVisible()
	await settle(page)
}

function sseMessage(ts: string, text: string) {
	return {
		event: 'message_created',
		data: {
			channel_id: 'C01GENERAL',
			payload: {
				channel_id: 'C01GENERAL',
				ts,
				bot_id: 'B01BOT',
				username: 'nuage-bot',
				text,
				created_at: new Date().toISOString(),
				updated_at: new Date().toISOString()
			}
		}
	}
}

test.describe('Nuage Notify (@nuage/ui) スマホ E2E', () => {
	test.beforeEach(async ({ page }) => {
		await setupRoutes(page)
	})

	test('viewport・ステータスバー・書体の設定と、入力欄が 16px 以上であること', async ({ page }) => {
		await page.goto('/')
		const viewport = await page.locator('meta[name="viewport"]').getAttribute('content')
		expect(viewport).toContain('maximum-scale=1')
		expect(viewport).toContain('user-scalable=no')
		expect(viewport).toContain('viewport-fit=cover')
		// black-translucent だと iOS のホーム画面 PWA で画面の下に描けない帯が残る (iPhone 実機で確認)
		expect(await page.locator('meta[name="apple-mobile-web-app-status-bar-style"]').getAttribute('content')).toBe('black')

		const fontSize = (loc: Locator) => loc.evaluate((el) => parseFloat(getComputedStyle(el).fontSize))
		expect(await fontSize(page.locator('#passphrase-input'))).toBeGreaterThanOrEqual(16)
		// Nuage UI 規約の書体 (IBM Plex) と、ダーク専用の背景
		const body = await page.evaluate(() => ({
			font: getComputedStyle(document.body).fontFamily,
			bg: getComputedStyle(document.body).backgroundColor
		}))
		expect(body.font).toContain('IBM Plex Sans')
		expect(body.bg).toBe('rgb(25, 24, 22)')

		await page.locator('#passphrase-input').fill('nuage-preview')
		await page.getByRole('button', { name: 'ログイン' }).click()
		await expect(page.locator('.nui-tabbar')).toBeVisible()
		expect(await fontSize(page.locator('.nui-search-input').filter({ visible: true }))).toBeGreaterThanOrEqual(16)

		await openGeneral(page)
		expect(await fontSize(page.locator('.composer-input').filter({ visible: true }))).toBeGreaterThanOrEqual(16)
		// 本文は規約の 14px
		expect(await fontSize(page.locator('#chat-msg-1700000080\\.000100 .msg-body'))).toBe(14)
	})

	test('タブが 3 つあり、未読と発生中のアラートの数がバッジに出て、開いたチャンネルは既読になる', async ({ page }) => {
		await login(page)
		await expect(page.locator('.nui-tabbar-item')).toHaveCount(3)
		await expect(page.locator('.nui-tabbar-item[data-tab="home"] .nui-badge')).toHaveText('2')
		await expect(page.locator('.nui-tabbar-item[data-tab="activity"] .nui-badge')).toHaveText('1')

		await openGeneral(page)
		await backButton(page).click()
		await settle(page)
		await expect(page.locator('.nui-tabbar-item[data-tab="home"] .nui-badge')).toHaveCount(0)
	})

	test('チャンネルを開いている間はタブバーを隠し、ホームは DOM に残り、戻るとホームが見える', async ({ page }) => {
		await login(page)
		for (const name of ['general', 'alertmanager', 'general']) {
			await channelItem(page, name).click()
			await settle(page)
			await expect(page).toHaveURL(/\/tabs\/home\/channels\//)
			await expect(page.locator('.nui-tabbar')).toHaveCount(0)
			await expect(page.locator('.channel-item').filter({ hasText: 'alertmanager' })).toHaveCount(1)
			await backButton(page).click()
			await settle(page)
			await expect(page).toHaveURL(/\/tabs\/home$/)
			await expect(channelItem(page, name)).toBeVisible()
			await expect(page.locator('.nui-tabbar')).toBeVisible()
		}
	})

	test('チャンネルを開くと一番下が見え、画像の読み込み後も一番下に留まる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const scroller = chatScroller(page)
		await expect(async () => {
			const loaded = await page.locator('.slack-inline-image').first().evaluate((el) => {
				const img = el as HTMLImageElement
				return img.complete && img.naturalHeight > 0
			})
			expect(loaded).toBe(true)
			expect((await scrollMetrics(scroller)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 5000 })
	})

	test('上へスクロールして古い履歴を読み込んでも、見ていたメッセージの位置が保たれる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const scroller = chatScroller(page)
		const anchor = page.locator('#chat-msg-1700000041\\.000100')
		await expect(anchor).toBeVisible()
		await scrollTo(scroller, 0)
		await expect(page.locator('#chat-msg-1700000011\\.000100')).toHaveCount(1)
		const anchorTop = await anchor.evaluate((el) => el.getBoundingClientRect().top)
		expect(anchorTop).toBeGreaterThan(-40)
		expect(anchorTop).toBeLessThan(400)
	})

	test('上を読んでいる時の新着は「新着あり」を出し、一番下なら自動で追いかける', async ({ page, request }) => {
		await login(page)
		await openGeneral(page)
		const scroller = chatScroller(page)
		await scrollTo(scroller, 200)
		await page.waitForTimeout(100)
		const before = (await scrollMetrics(scroller)).top

		await request.post('/__test/sse', { data: sseMessage('1700009001.000100', 'SSE 新着テスト #1') })
		const button = page.locator('.new-messages-btn').filter({ visible: true })
		await expect(button).toBeVisible()
		expect(Math.abs((await scrollMetrics(scroller)).top - before)).toBeLessThan(4)

		await button.click()
		await expect(async () => {
			expect((await scrollMetrics(scroller)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
		await expect(button).toHaveCount(0)

		await request.post('/__test/sse', { data: sseMessage('1700009002.000100', 'SSE 新着テスト #2') })
		await expect(page.getByText('SSE 新着テスト #2')).toBeVisible()
		await expect(async () => {
			expect((await scrollMetrics(scroller)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
	})

	test('画面の高さが縮んでも (キーボード表示を想定)、一番下を見ていれば一番下に留まる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const scroller = chatScroller(page)
		const size = page.viewportSize()!
		await page.setViewportSize({ width: size.width, height: Math.round(size.height * 0.55) })
		await expect(async () => {
			expect((await scrollMetrics(scroller)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
	})

	test('メッセージを送ると一覧の一番下に出る', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const composer = page.locator('.composer-input').filter({ visible: true })
		await composer.fill('E2E からの送信テスト')
		await page.getByRole('button', { name: '送信' }).filter({ visible: true }).click()
		await expect(page.getByText('E2E からの送信テスト')).toBeVisible()
		await expect(composer).toHaveValue('')
		await expect(async () => {
			expect((await scrollMetrics(chatScroller(page))).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
	})

	test('返信のリンクからスレッドを開き、戻るでチャンネルへ戻れる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		await page.getByText('25 件の返信').click()
		await expect(page).toHaveURL(/\/tabs\/home\/channels\/C01GENERAL\/threads\/1700000080\.000100$/)
		await expect(page.getByText('スレッド返信 #25')).toBeVisible()
		await settle(page)
		await backButton(page).click()
		await settle(page)
		await expect(page).toHaveURL(/\/tabs\/home\/channels\/C01GENERAL$/)
	})

	test('ホーム画面 PWA では、左端からのスワイプで戻れる', async ({ page }) => {
		// ホーム画面で開いた PWA を再現する (navigator.standalone)
		await page.addInitScript(() => Object.defineProperty(navigator, 'standalone', { value: true }))
		await login(page)
		await openGeneral(page)
		const edge = page.locator('.nui-stack-edge').filter({ visible: true })
		await expect(edge).toHaveCount(1)
		const height = page.viewportSize()!.height
		await page.mouse.move(4, height / 2)
		await page.mouse.down()
		for (let x = 20; x <= 300; x += 40) await page.mouse.move(x, height / 2)
		await page.mouse.up()
		await expect(page).toHaveURL(/\/tabs\/home$/)
		await settle(page)
		await expect(channelItem(page, 'general')).toBeVisible()
	})

	test('Push 通知の URL (/channels/…/threads/…) は、アラートのスレッドと詳細を開く', async ({ page }) => {
		await login(page)
		await page.goto('/channels/C01ALERTS/threads/1700002000.000100')
		await expect(page).toHaveURL(/\/tabs\/home\/channels\/C01ALERTS\/threads\/1700002000\.000100$/)
		await expect(page.locator('.history-detail-panel').filter({ visible: true })).toBeVisible()
		await expect(page.getByText('alertname:').filter({ visible: true })).toBeVisible()
		// 直接開いた画面からも、戻るでチャンネルへ戻れる
		await settle(page)
		await backButton(page).click()
		await settle(page)
		await expect(page).toHaveURL(/\/tabs\/home\/channels\/C01ALERTS$/)
	})

	test('通知タブで絞り込み、項目からアラートのスレッドへ移れる', async ({ page }) => {
		await login(page)
		await page.locator('.nui-tabbar-item[data-tab="activity"]').click()
		await settle(page)
		const segment = (label: string) => page.getByRole('tab', { name: new RegExp(`^${label}`) }).filter({ visible: true })
		const items = page.locator('.activity-item').filter({ visible: true })
		await expect(items).toHaveCount(3)

		await segment('発生中').click()
		await expect(items).toHaveCount(1)
		await expect(items.first()).toContainText('KubeJobFailed')

		await segment('一般').click()
		await expect(items).toHaveCount(1)
		await expect(items.first()).toContainText('monitoring-pwa を同期した')

		await segment('発生中').click()
		await expect(items).toHaveCount(1)
		await items.first().click()
		await expect(page).toHaveURL(/\/tabs\/activity\/channels\/C01ALERTS\/threads\/1700002000\.000100$/)
	})

	test('ホームの検索から該当するスレッドを開ける', async ({ page }) => {
		await login(page)
		await page.locator('.nui-search-input').filter({ visible: true }).fill('メッセージ')
		const result = page.locator('.search-result-item').filter({ visible: true })
		await expect(result).toHaveCount(1)
		await result.click()
		await expect(page).toHaveURL(/\/channels\/C01GENERAL\/threads\/1700000080\.000100$/)
	})

	test('中身の無いチャンネルは、その旨を表示する', async ({ page }) => {
		await page.route('**/api/channels/C01ALERTS/messages**', (route) =>
			route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: [], has_more: false }) })
		)
		await login(page)
		await channelItem(page, 'alertmanager').click()
		await expect(page.locator('.nui-empty').filter({ visible: true })).toContainText('まだメッセージはない')
	})

	test('DM はホームのチャンネルの下に並ぶ', async ({ page }) => {
		await login(page)
		await expect(page.locator('.nui-list-title').filter({ hasText: 'ダイレクトメッセージ' })).toBeVisible()
		await channelItem(page, 'nuage-bot').click()
		await expect(page).toHaveURL(/\/channels\/D01BOT$/)
	})

	test('設定のログアウトは確認してから行う', async ({ page }) => {
		await login(page)
		await page.locator('.nui-tabbar-item[data-tab="settings"]').click()
		await settle(page)
		await page.getByRole('button', { name: 'ログアウト' }).filter({ visible: true }).first().click()
		const dialog = page.getByRole('dialog')
		await expect(dialog).toContainText('ログアウトする？')
		await dialog.getByRole('button', { name: 'キャンセル' }).click()
		await expect(dialog).toHaveCount(0)
	})

	test('未知のパスはホームへ移る', async ({ page }) => {
		await login(page)
		await page.goto('/no-such-page')
		await expect(page).toHaveURL(/\/tabs\/home$/)
	})

	test('PC・タブレットの幅では、一覧・チャンネル・スレッドを左右に並べる', async ({ page }) => {
		await login(page)
		await page.setViewportSize({ width: 1280, height: 800 })
		await page.goto('/tabs/home/channels/C01ALERTS/threads/1700002000.000100')
		await expect(page.locator('.nui-split-sidebar .channel-item').filter({ hasText: 'alertmanager' })).toHaveAttribute(
			'data-selected',
			'true'
		)
		await expect(page.locator('.nui-split-main .nui-toolbar-title-main')).toHaveText('# alertmanager')
		await expect(page.locator('.nui-split-aside .history-detail-panel')).toBeVisible()
		await expect(page.locator('.nui-tabbar')).toHaveCount(0)
		// スレッドを閉じるとチャンネルだけになる
		await page.getByRole('button', { name: 'スレッドを閉じる' }).click()
		await expect(page).toHaveURL(/\/tabs\/home\/channels\/C01ALERTS$/)
		await expect(page.locator('.nui-split-aside')).toHaveCount(0)
	})

	test('ホームの検索では入力直後 (デバウンス中) から読み込み中を表示する', async ({ page }) => {
		await page.route('**/api/search**', async (route) => {
			await new Promise((resolve) => setTimeout(resolve, 250))
			await route.continue()
		})
		await login(page)
		await page.locator('.nui-search-input').filter({ visible: true }).fill('メッセージ')
		await expect(page.locator('.nui-list-header').filter({ hasText: '検索結果' }).locator('.nui-spinner')).toBeVisible()
		await expect(page.locator('.search-result-item').filter({ visible: true })).toHaveCount(1)
	})

	test('通知とチャンネルの取得失敗時は空表示に化けずエラーと再読み込み導線を出す', async ({ page }) => {
		let activityFailed = false
		await page.route('**/api/activity?filter=all**', async (route) => {
			if (!activityFailed) {
				activityFailed = true
				await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'boom' }) })
				return
			}
			await route.continue()
		})
		let channelFailed = false
		await page.route('**/api/channels/C01ALERTS/messages**', async (route) => {
			if (!channelFailed) {
				channelFailed = true
				await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'boom' }) })
				return
			}
			await route.continue()
		})

		await login(page)
		await page.locator('.nui-tabbar-item[data-tab="activity"]').click()
		await settle(page)
		await expect(page.locator('.nui-empty').filter({ visible: true })).toContainText('通知を読み込めなかった。')
		await page.getByRole('button', { name: '再読み込み' }).filter({ visible: true }).click()
		await expect(page.locator('.activity-item').filter({ visible: true })).toHaveCount(3)

		await page.locator('.nui-tabbar-item[data-tab="home"]').click()
		await settle(page)
		await channelItem(page, 'alertmanager').click()
		await expect(page.locator('.nui-empty').filter({ visible: true })).toContainText('メッセージを読み込めなかった。')
		await page.getByRole('button', { name: '再読み込み' }).filter({ visible: true }).click()
		await expect(page.getByText('KubeJobFailed').filter({ visible: true })).toBeVisible()
	})

	test('設定のプッシュ通知スイッチは SW 未登録でもタイムアウト後に確認中状態が解除される', async ({ page }) => {
		await login(page)
		await page.locator('.nui-tabbar-item[data-tab="settings"]').click()
		const pushToggle = page.getByRole('switch', { name: 'プッシュ通知を受け取る' }).filter({ visible: true })
		await expect(pushToggle).toBeVisible()
		await expect(pushToggle).toHaveAttribute('aria-busy', 'true')
		await settle(page)
		await expect(pushToggle).not.toHaveAttribute('aria-busy', 'true', { timeout: 5000 })
		await expect(pushToggle).toHaveAttribute('data-checked', 'false')
	})
})
