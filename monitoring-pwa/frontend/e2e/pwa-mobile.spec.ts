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

// Ionic の画面遷移アニメーション中は ion-router-outlet がクリックを受け止めるため、終わるまで待つ
async function settle(page: Page) {
	await page.waitForFunction(() => !document.querySelector('ion-router-outlet .ion-page.ion-page-invisible'))
	await page.waitForTimeout(650)
}

// 今いちばん手前に見えている画面の戻るボタン
function backButton(page: Page): Locator {
	return page.locator('ion-back-button').filter({ visible: true }).last()
}

async function login(page: Page) {
	await page.goto('/')
	const input = page.locator('#passphrase-input input')
	await expect(input).toBeVisible()
	await input.fill('nuage-preview')
	await page.getByRole('button', { name: 'ログイン' }).click()
	await expect(page.locator('ion-tab-bar')).toBeVisible()
	await settle(page)
}

// Ionic は前の画面を DOM に残すため、今見えている画面に絞って探す
function visibleChat(page: Page): Locator {
	return page.locator('ion-content.chat-content').filter({ visible: true })
}

async function scrollMetrics(content: Locator) {
	return content.evaluate(async (el) => {
		const scroller = await (el as HTMLIonContentElement).getScrollElement()
		return {
			top: scroller.scrollTop,
			distanceToBottom: scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight
		}
	})
}

async function scrollTo(content: Locator, top: number) {
	await content.evaluate(async (el, y) => {
		const scroller = await (el as HTMLIonContentElement).getScrollElement()
		scroller.scrollTop = y
		scroller.dispatchEvent(new Event('scroll'))
	}, top)
}

async function openGeneral(page: Page) {
	await page.locator('ion-item').filter({ hasText: 'general' }).filter({ visible: true }).click()
	await expect(page.locator('#chat-msg-1700000080\\.000100')).toBeVisible()
	await settle(page)
}

function sseMessage(id: number, ts: string, text: string) {
	return {
		event: 'message_created',
		data: {
			channel_id: 'C01GENERAL',
			payload: {
				id,
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

test.describe('Nuage Notify (Ionic) スマホ E2E', () => {
	test.beforeEach(async ({ page }) => {
		await setupRoutes(page)
	})

	test('viewport・ステータスバーの設定と、入力欄が 16px 以上であること', async ({ page }) => {
		await page.goto('/')
		const viewport = await page.locator('meta[name="viewport"]').getAttribute('content')
		expect(viewport).toContain('maximum-scale=1')
		expect(viewport).toContain('user-scalable=no')
		expect(viewport).toContain('viewport-fit=cover')
		// black-translucent だと iOS のホーム画面 PWA で画面の下に描けない帯が残る (iPhone 実機で確認)
		const statusBar = await page
			.locator('meta[name="apple-mobile-web-app-status-bar-style"]')
			.getAttribute('content')
		expect(statusBar).toBe('black')

		const fontSize = (loc: Locator) => loc.evaluate((el) => parseFloat(getComputedStyle(el).fontSize))
		expect(await fontSize(page.locator('#passphrase-input input'))).toBeGreaterThanOrEqual(16)

		await page.locator('#passphrase-input input').fill('nuage-preview')
		await page.getByRole('button', { name: 'ログイン' }).click()
		await expect(page.locator('ion-tab-bar')).toBeVisible()
		expect(await fontSize(page.locator('ion-searchbar input').filter({ visible: true }))).toBeGreaterThanOrEqual(16)

		await openGeneral(page)
		const composer = page.locator('.composer-input textarea').filter({ visible: true })
		expect(await fontSize(composer)).toBeGreaterThanOrEqual(16)

		// 本文は 14px 前後 (基準 13px × 1.07rem)。情報量を多くするため Slack よりやや小さめ
		const bodySize = await fontSize(page.locator('#chat-msg-1700000080\\.000100 .msg-body'))
		expect(bodySize).toBeGreaterThanOrEqual(13)
		expect(bodySize).toBeLessThanOrEqual(15)
	})

	test('タブが 3 つ (ホーム・通知・設定) あり、未読と発生中のアラートの数がバッジに出る', async ({ page }) => {
		await login(page)
		const tabs = page.locator('ion-tab-button')
		await expect(tabs).toHaveCount(3)
		await expect(page.locator('ion-tab-button[tab="home"] ion-badge')).toHaveText('2')
		await expect(page.locator('ion-tab-button[tab="activity"] ion-badge')).toHaveText('1')

		// チャンネルを開くと既読になり、ホームのバッジが消える
		await openGeneral(page)
		await backButton(page).click()
		await settle(page)
		await expect(page.locator('ion-tab-button[tab="home"] ion-badge')).toHaveCount(0)
	})

	test('チャンネルを開くと一番下が見え、画像の読み込み後も一番下に留まる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const content = visibleChat(page)
		await expect(async () => {
			const loaded = await page.locator('.slack-inline-image').first().evaluate((el) => {
				const img = el as HTMLImageElement
				return img.complete && img.naturalHeight > 0
			})
			expect(loaded).toBe(true)
			expect((await scrollMetrics(content)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 5000 })
	})

	test('上へスクロールして古い履歴を読み込んでも、見ていたメッセージの位置が保たれる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const content = visibleChat(page)
		const anchor = page.locator('#chat-msg-1700000041\\.000100')
		await expect(anchor).toBeVisible()

		await scrollTo(content, 0)
		await expect(page.locator('#chat-msg-1700000011\\.000100')).toHaveCount(1)
		const anchorTop = await anchor.evaluate((el) => el.getBoundingClientRect().top)
		// 先頭に足しても、見ていた (先頭だった) メッセージは画面の上の方に留まる
		expect(anchorTop).toBeGreaterThan(-40)
		expect(anchorTop).toBeLessThan(400)
	})

	test('上を読んでいる時の新着は「新着あり」を出し、一番下なら自動で追いかける', async ({ page, request }) => {
		await login(page)
		await openGeneral(page)
		const content = visibleChat(page)
		await scrollTo(content, 200)
		await page.waitForTimeout(100)
		const before = (await scrollMetrics(content)).top

		await request.post('/__test/sse', { data: sseMessage(5001, '1700009001.000100', 'SSE 新着テスト #1') })
		const button = page.locator('.new-messages-btn').filter({ visible: true })
		await expect(button).toBeVisible()
		expect(Math.abs((await scrollMetrics(content)).top - before)).toBeLessThan(4)

		await button.click()
		await expect(async () => {
			expect((await scrollMetrics(content)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
		await expect(button).toHaveCount(0)

		await request.post('/__test/sse', { data: sseMessage(5002, '1700009002.000100', 'SSE 新着テスト #2') })
		await expect(page.getByText('SSE 新着テスト #2')).toBeVisible()
		await expect(async () => {
			expect((await scrollMetrics(content)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
		await expect(page.locator('.new-messages-btn').filter({ visible: true })).toHaveCount(0)
	})

	test('画面の高さが縮んでも (キーボード表示を想定)、一番下を見ていれば一番下に留まる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const content = visibleChat(page)
		const size = page.viewportSize()!
		await page.setViewportSize({ width: size.width, height: Math.round(size.height * 0.55) })
		await expect(async () => {
			expect((await scrollMetrics(content)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
	})

	test('メッセージを送ると一覧の一番下に出る', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		const composer = page.locator('.composer-input textarea').filter({ visible: true })
		await composer.fill('E2E からの送信テスト')
		await page.getByRole('button', { name: '送信' }).filter({ visible: true }).click()
		await expect(page.getByText('E2E からの送信テスト')).toBeVisible()
		await expect(composer).toHaveValue('')
		const content = visibleChat(page)
		await expect(async () => {
			expect((await scrollMetrics(content)).distanceToBottom).toBeLessThan(8)
		}).toPass({ timeout: 3000 })
	})

	test('返信のリンクからスレッドを開き、戻るでチャンネルへ戻れる', async ({ page }) => {
		await login(page)
		await openGeneral(page)
		await page.getByText('25 件の返信').click()
		await expect(page).toHaveURL(/\/channels\/C01GENERAL\/threads\/1700000080\.000100$/)
		await expect(page.getByText('スレッド返信 #25')).toBeVisible()
		await settle(page)
		await backButton(page).click()
		await expect(page).toHaveURL(/\/channels\/C01GENERAL$/)
	})

	test('ディープリンク /alerts/:id は、アラートのスレッドと詳細を開く', async ({ page }) => {
		await login(page)
		await page.goto('/alerts/42')
		await expect(page).toHaveURL(/\/channels\/C01ALERTS\/threads\/1700002000\.000100$/)
		await expect(page.locator('.history-detail-panel').filter({ visible: true })).toBeVisible()
		await expect(page.getByText('alertname:').filter({ visible: true })).toBeVisible()
	})

	test('通知タブで絞り込み、項目からアラートのスレッドへ移れる', async ({ page }) => {
		await login(page)
		await page.locator('ion-tab-button[tab="activity"]').click()
		await settle(page)
		const segment = (label: string) => page.locator('ion-segment-button').filter({ hasText: label })
		const items = page.locator('.activity-item').filter({ visible: true })
		await expect(items).toHaveCount(3)

		await segment('発生中').click()
		await expect(items).toHaveCount(1)
		await expect(items.first()).toContainText('KubeJobFailed')

		await segment('一般').click()
		await expect(items).toHaveCount(1)
		await expect(items.first()).toContainText('Argo CD')

		await segment('発生中').click()
		await items.first().click()
		await expect(page).toHaveURL(/\/channels\/C01ALERTS\/threads\/1700002000\.000100$/)
	})

	test('ホームの検索から該当するスレッドを開ける', async ({ page }) => {
		await login(page)
		await page.locator('ion-searchbar input').filter({ visible: true }).fill('メッセージ')
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
		await page.locator('ion-item').filter({ hasText: 'alertmanager' }).filter({ visible: true }).click()
		await expect(page.locator('.channel-empty').filter({ visible: true })).toContainText('まだメッセージはない')
	})

	test('DM はホームのチャンネルの下に並び、旧 DM タブのパスはホームへ移る', async ({ page }) => {
		await login(page)
		await expect(page.locator('ion-list-header').filter({ hasText: 'ダイレクトメッセージ' })).toBeVisible()
		await page.locator('ion-item').filter({ hasText: 'nuage-bot' }).filter({ visible: true }).click()
		await expect(page).toHaveURL(/\/channels\/D01BOT$/)
		await page.goto('/tabs/dms')
		await expect(page).toHaveURL(/\/tabs\/home$/)
	})

	test('ホーム画面 PWA 用の旧パスと未知のパスはホームへ移る', async ({ page }) => {
		await login(page)
		await page.goto('/settings')
		await expect(page).toHaveURL(/\/tabs\/settings$/)
		await page.goto('/no-such-page')
		await expect(page).toHaveURL(/\/tabs\/home$/)
	})
})
