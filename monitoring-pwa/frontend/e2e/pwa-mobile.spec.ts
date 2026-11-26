import { expect, test, type CDPSession, type Page } from '@playwright/test'

// テスト用 240x220 SVG (外部通信を避けつつ遅延読込の高さ変化と画像ライトボックスをテストする)
const MOCK_SVG = Buffer.from(
	'<svg xmlns="http://www.w3.org/2000/svg" width="240" height="220" viewBox="0 0 240 220"><rect width="240" height="220" fill="#2c2a26"/><circle cx="120" cy="110" r="60" fill="#e07a5f"/></svg>',
	'utf-8'
)

async function setupRoutes(page: Page) {
	await page.route('https://mock-assets.local/**', async (route) => {
		// 初期スクロール後に遅延ロードで高さが伸びる状況を再現する
		await new Promise((resolve) => setTimeout(resolve, 120))
		await route.fulfill({
			status: 200,
			contentType: 'image/svg+xml',
			body: MOCK_SVG
		})
	})
}

async function loginAsTestUser(page: Page) {
	await page.goto('/')
	const input = page.locator('#passphrase-input')
	await expect(input).toBeVisible()
	await input.fill('nuage-preview')
	await page.getByRole('button', { name: 'ログイン' }).click()
	await expect(page.locator('.chat-sidebar')).toBeVisible()
}

async function performEdgeSwipeRight(
	page: Page,
	startX: number,
	endX: number,
	steps = 10
) {
	const viewport = page.viewportSize()
	if (!viewport) throw new Error('viewport is missing')
	const y = Math.round(viewport.height * 0.5)
	await page.mouse.move(startX, y)
	await page.mouse.down()
	await page.mouse.move(endX, y, { steps })
	await page.mouse.up()
}

async function performTouchEdgeSwipeRight(
	page: Page,
	cdp: CDPSession,
	startX: number,
	endX: number,
	steps = 10,
	stepDelayMs = 10,
	onMidMove?: () => Promise<void>
) {
	const viewport = page.viewportSize()
	if (!viewport) throw new Error('viewport is missing')
	const y = Math.round(viewport.height * 0.5)

	await cdp.send('Input.dispatchTouchEvent', {
		type: 'touchStart',
		touchPoints: [{ x: startX, y, id: 1 }]
	})
	for (let i = 1; i <= steps; i++) {
		const x = Math.round(startX + ((endX - startX) * i) / steps)
		await cdp.send('Input.dispatchTouchEvent', {
			type: 'touchMove',
			touchPoints: [{ x, y, id: 1 }]
		})
		if (onMidMove && i === Math.ceil(steps / 2)) {
			await onMidMove()
		}
		if (stepDelayMs > 0) {
			await page.waitForTimeout(stepDelayMs)
		}
	}
	await cdp.send('Input.dispatchTouchEvent', {
		type: 'touchEnd',
		touchPoints: []
	})
}

test.describe('PWA スマホ最適化 E2E', () => {
	test.beforeEach(async ({ page }) => {
		await setupRoutes(page)
	})

	test('viewport 設定とすべての入力欄の font-size >= 16px、および body スクロール抑止を検証する', async ({
		page
	}) => {
		await page.goto('/')

		const viewportContent = await page
			.locator('meta[name="viewport"]')
			.getAttribute('content')
		expect(viewportContent).toContain('maximum-scale=1')
		expect(viewportContent).toContain('user-scalable=no')
		expect(viewportContent).toContain('interactive-widget=resizes-content')
		expect(viewportContent).toContain('viewport-fit=cover')

		const loginInputFontSize = await page
			.locator('#passphrase-input')
			.evaluate((el) => parseFloat(getComputedStyle(el).fontSize))
		expect(loginInputFontSize).toBeGreaterThanOrEqual(16)

		// ログインしてチャンネルへ入る
		await page.locator('#passphrase-input').fill('nuage-preview')
		await page.getByRole('button', { name: 'ログイン' }).click()
		await page.getByRole('button', { name: /general/ }).click()

		const composer = page.locator('.chat-main .composer-textarea')
		await expect(composer).toBeVisible()
		const composerFontSize = await composer.evaluate((el) =>
			parseFloat(getComputedStyle(el).fontSize)
		)
		expect(composerFontSize).toBeGreaterThanOrEqual(16)

		// スレッド入力欄の font-size チェック
		await page
			.locator('#chat-msg-1700000080\\.000100 .thread-open-btn')
			.click()
		const threadComposer = page.locator('.thread-pane .composer-textarea')
		await expect(threadComposer).toBeVisible()
		const threadFontSize = await threadComposer.evaluate((el) =>
			parseFloat(getComputedStyle(el).fontSize)
		)
		expect(threadFontSize).toBeGreaterThanOrEqual(16)

		// 検索モーダル入力欄の font-size チェック
		await page.locator('.thread-header .icon-btn').first().click()
		await page.getByRole('button', { name: '検索' }).click()
		const searchInput = page.locator('.search-input')
		await expect(searchInput).toBeVisible()
		const searchFontSize = await searchInput.evaluate((el) =>
			parseFloat(getComputedStyle(el).fontSize)
		)
		expect(searchFontSize).toBeGreaterThanOrEqual(16)
		await page.getByRole('button', { name: '閉じる' }).click()

		// body と html がスクロールしないこと、各領域に overscroll-behavior: contain があること
		const layoutCheck = await page.evaluate(() => {
			const htmlStyle = getComputedStyle(document.documentElement)
			const bodyStyle = getComputedStyle(document.body)
			const timelineStyle = getComputedStyle(document.querySelector('.chat-timeline')!)
			const sidebarStyle = getComputedStyle(document.querySelector('.sidebar-scroll')!)
			window.scrollTo(0, 200)
			return {
				docScrollable: document.documentElement.scrollHeight > window.innerHeight,
				bodyScrollable: document.body.scrollHeight > window.innerHeight,
				scrollY: window.scrollY,
				htmlOverflowY: htmlStyle.overflowY,
				bodyOverflowY: bodyStyle.overflowY,
				htmlOverscroll: htmlStyle.overscrollBehaviorY,
				timelineOverscroll: timelineStyle.overscrollBehaviorY,
				sidebarOverscroll: sidebarStyle.overscrollBehaviorY
			}
		})
		expect(layoutCheck.docScrollable).toBe(false)
		expect(layoutCheck.bodyScrollable).toBe(false)
		expect(layoutCheck.scrollY).toBe(0)
		expect(layoutCheck.htmlOverflowY).toBe('hidden')
		expect(layoutCheck.bodyOverflowY).toBe('hidden')
		expect(layoutCheck.htmlOverscroll).toBe('none')
		expect(layoutCheck.timelineOverscroll).toBe('contain')
		expect(layoutCheck.sidebarOverscroll).toBe('contain')

		// 触り心地: UI枠は選択不可、メッセージ本文とコードは選択可能
		const selectCheck = await page.evaluate(() => {
			const header = document.querySelector('.chat-header')!
			const body = document.querySelector('.bubble-body')!
			const code = document.querySelector('.slack-codeblock')!
			const getSelect = (el: Element) => {
				const st = getComputedStyle(el)
				return st.userSelect || st.webkitUserSelect
			}
			return {
				headerSelect: getSelect(header),
				bodySelect: getSelect(body),
				codeSelect: getSelect(code)
			}
		})
		expect(selectCheck.headerSelect).toBe('none')
		expect(selectCheck.bodySelect).toBe('text')
		expect(selectCheck.codeSelect).toBe('text')
	})

	test('チャンネルとスレッドを開いたときに一番下が見え、画像タップで全画面拡大できる', async ({
		page
	}) => {
		await loginAsTestUser(page)
		await page.getByRole('button', { name: /general/ }).click()

		// チャンネル最下部（メッセージ #080）が見え、遅延画像ロード後も最下部を維持すること
		const latestMsg = page.locator('#chat-msg-1700000080\\.000100')
		await expect(latestMsg).toBeVisible()
		await expect(async () => {
			const loaded = await page.locator('.slack-inline-image').first().evaluate((el) => {
				const img = el as HTMLImageElement
				return img.complete && img.naturalHeight >= 200
			})
			expect(loaded).toBe(true)
			const metrics = await page.locator('.chat-timeline').evaluate((el) => ({
				distanceFromBottom: el.scrollHeight - el.scrollTop - el.clientHeight
			}))
			expect(metrics.distanceFromBottom).toBeLessThan(8)
		}).toPass()

		// 画像をタップして全画面拡大できること
		const imgTrigger = page.locator('.slack-image-trigger').first()
		await imgTrigger.scrollIntoViewIfNeeded()
		await imgTrigger.click()
		const lightbox = page.locator('.image-lightbox-backdrop')
		await expect(lightbox).toBeVisible()
		await page.getByRole('button', { name: '画像を拡大する' }).click()
		await expect(page.locator('.image-lightbox-img.zoomed')).toBeVisible()
		await page.getByRole('button', { name: '画像を閉じる' }).click()
		await expect(lightbox).toBeHidden()

		// 最下部へ戻ってからスレッドを開き、最新返信 (#25) が見えること
		await page.locator('.chat-timeline').evaluate((el) => {
			el.scrollTop = el.scrollHeight
		})
		await latestMsg.locator('.thread-open-btn').click()
		await expect(page.locator('.thread-pane')).toBeVisible()
		await expect(page.getByText('スレッド返信 #25')).toBeVisible()
		await expect(async () => {
			const dist = await page
				.locator('.thread-scroll')
				.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight)
			expect(dist).toBeLessThan(8)
		}).toPass()
	})

	test('古い履歴を読み込んでも見ていたメッセージ位置が保たれる', async ({ page }) => {
		await loginAsTestUser(page)
		await page.getByRole('button', { name: /general/ }).click()

		const timeline = page.locator('.chat-timeline')
		// まず最新位置の描画完了を待つ
		await expect(page.locator('#chat-msg-1700000080\\.000100')).toBeVisible()

		// 古い履歴 API リクエストを一旦止めて、読み込み直前の表示位置を正確に計測できるようにする
		let releaseOlderRequest!: () => void
		let notifyOlderRequestStarted!: () => void
		const olderRequestStarted = new Promise<void>((resolve) => {
			notifyOlderRequestStarted = resolve
		})
		await page.route('**/api/channels/*/messages*', async (route) => {
			const reqUrl = route.request().url()
			if (!reqUrl.includes('before=')) {
				await route.continue()
				return
			}
			notifyOlderRequestStarted()
			await new Promise<void>((done) => {
				releaseOlderRequest = done
			})
			await route.continue()
		})

		// 上端付近へスクロールして古い履歴の自動読み込みを発火させ、先頭メッセージ #041 の位置を記録する
		const targetMsg = page.locator('#chat-msg-1700000041\\.000100')
		await timeline.evaluate((el) => {
			el.scrollTop = 20
		})
		await olderRequestStarted
		await expect(targetMsg).toBeVisible()

		const beforeTop = await targetMsg.evaluate((el) => el.getBoundingClientRect().top)
		releaseOlderRequest()

		// 古いメッセージ #011 が先頭に追加されたことを確認
		await expect(page.locator('#chat-msg-1700000011\\.000100')).toBeAttached()

		// 元のメッセージ #041 の表示位置が大きく飛んでいないこと
		const afterTop = await targetMsg.evaluate((el) => el.getBoundingClientRect().top)
		expect(Math.abs(afterTop - beforeTop)).toBeLessThan(8)
	})

	test('上を読んでいる時は新着で自動スクロールせず「新着あり ↓」ボタンが出る', async ({
		page,
		request
	}) => {
		await loginAsTestUser(page)
		await page.getByRole('button', { name: /general/ }).click()
		await expect(page.locator('#chat-msg-1700000080\\.000100')).toBeVisible()

		const timeline = page.locator('.chat-timeline')
		// 上の位置（メッセージ #045 付近）へスクロールする
		await timeline.evaluate((el) => {
			el.scrollTop = 240
		})
		const scrollTopBefore = await timeline.evaluate((el) => el.scrollTop)

		// SSE で新着メッセージを配信する
		await request.post('/__test/sse', {
			data: {
				event: 'message_created',
				data: {
					channel_id: 'C01GENERAL',
					payload: {
						id: 5001,
						channel_id: 'C01GENERAL',
						ts: '1700009001.000100',
						bot_id: 'B01BOT',
						username: 'nuage-bot',
						text: 'SSE リアルタイム新着テスト #1',
						created_at: new Date().toISOString(),
						updated_at: new Date().toISOString()
					}
				}
			}
		})

		const newMsgBtn = page.getByRole('button', { name: '最新のメッセージへ移動' })
		await expect(newMsgBtn).toBeVisible()
		await expect(newMsgBtn).toContainText('新着あり ↓')

		// 上を読んでいるため勝手にスクロールしていないこと
		const scrollTopAfterSse = await timeline.evaluate((el) => el.scrollTop)
		expect(Math.abs(scrollTopAfterSse - scrollTopBefore)).toBeLessThan(4)

		// ボタン押下で一番下へ移動し、新着メッセージが表示されボタンが消えること
		await newMsgBtn.click()
		await expect(newMsgBtn).toBeHidden()
		await expect(page.getByText('SSE リアルタイム新着テスト #1')).toBeVisible()
		const distAfterClick = await timeline.evaluate(
			(el) => el.scrollHeight - el.scrollTop - el.clientHeight
		)
		expect(distAfterClick).toBeLessThan(8)

		// 一番下を見ている状態でさらに新着が来たら、ボタンが出ずに自動追従すること
		await request.post('/__test/sse', {
			data: {
				event: 'message_created',
				data: {
					channel_id: 'C01GENERAL',
					payload: {
						id: 5002,
						channel_id: 'C01GENERAL',
						ts: '1700009002.000100',
						bot_id: 'B01BOT',
						username: 'nuage-bot',
						text: 'SSE リアルタイム新着テスト #2',
						created_at: new Date().toISOString(),
						updated_at: new Date().toISOString()
					}
				}
			}
		})
		await expect(page.getByText('SSE リアルタイム新着テスト #2')).toBeVisible()
		await expect(newMsgBtn).toBeHidden()
	})

	test('左端スワイプでスレッド→チャンネル→一覧へ戻れ、途中で放すと戻らない', async ({
		page
	}) => {
		await loginAsTestUser(page)

		// チャンネル一覧 → チャンネル → スレッドへ進む
		await page.getByRole('button', { name: /general/ }).click()
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-messages/)
		await expect(page).toHaveURL(/\/channels\/C01GENERAL$/)

		await page
			.locator('#chat-msg-1700000080\\.000100 .thread-open-btn')
			.click()
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-thread/)
		await expect(page).toHaveURL(/\/channels\/C01GENERAL\/threads\/1700000080\.000100$/)

		// わずかにスワイプしてゆっくり放した時は戻らないこと (10px -> 50px, ゆっくり)
		await performEdgeSwipeRight(page, 10, 50, 20)
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-thread/)

		// 左端から十分右へスワイプするとスレッドからチャンネルへ戻ること
		await performEdgeSwipeRight(page, 8, 250, 8)
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-messages/)
		await expect(page).toHaveURL(/\/channels\/C01GENERAL$/)

		// もう一度左端から右へスワイプするとチャンネルからチャンネル一覧へ戻ること
		await performEdgeSwipeRight(page, 8, 250, 8)
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-channels/)
		await expect(page).toHaveURL(/\/$/)

		// ブラウザの「進む」で再びチャンネルに戻れること (履歴整合)
		await page.goForward()
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-messages/)
		await expect(page).toHaveURL(/\/channels\/C01GENERAL$/)
	})

	test('ディープリンクとキーボード縮小時の最下部維持が機能する', async ({ page }) => {
		await loginAsTestUser(page)

		// /alerts/42 ディープリンクを開くと URL を維持したままスレッドが表示されること
		await page.goto('/alerts/42')
		await expect(page.locator('.thread-pane')).toBeVisible()
		await expect(page).toHaveURL(/\/alerts\/42$/)

		// スレッドから戻るとチャンネル画面へ移動すること
		await page.locator('.thread-header .mobile-only-btn').click()
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-messages/)
		await expect(page).toHaveURL(/\/channels\/C01GENERAL$/)

		// キーボード展開を模してビューポート高さを縮めても一番下が見えたままになること
		const timeline = page.locator('.chat-timeline')
		await expect(async () => {
			const dist = await timeline.evaluate(
				(el) => el.scrollHeight - el.scrollTop - el.clientHeight
			)
			expect(dist).toBeLessThan(8)
		}).toPass()

		const originalViewport = page.viewportSize()!
		await page.setViewportSize({
			width: originalViewport.width,
			height: Math.round(originalViewport.height * 0.55)
		})

		await expect(async () => {
			const distAfterShrink = await timeline.evaluate(
				(el) => el.scrollHeight - el.scrollTop - el.clientHeight
			)
			expect(distAfterShrink).toBeLessThan(8)
		}).toPass()
	})

	test('Touch Events による左端スワイプ戻りとスレッド・設定のスライドイン開始位置を検証する', async ({
		page
	}) => {
		await loginAsTestUser(page)
		const cdp = await page.context().newCDPSession(page)
		const viewport = page.viewportSize()!

		// 1. 設定画面を開くときのスライドイン開始位置が右端 (100%) から始まること
		await page.evaluate(() => {
			;(window as unknown as { __settingsStartTx?: number }).__settingsStartTx = undefined
			const obs = new MutationObserver((mutations) => {
				for (const m of mutations) {
					for (const node of Array.from(m.addedNodes)) {
						if (node instanceof HTMLElement && node.classList.contains('settings-pane')) {
							const tr = getComputedStyle(node).transform
							const match = tr.match(/matrix\([^,]+,[^,]+,[^,]+,[^,]+,([^,]+),/)
							;(window as unknown as { __settingsStartTx?: number }).__settingsStartTx = match
								? parseFloat(match[1])
								: 0
							obs.disconnect()
						}
					}
				}
			})
			obs.observe(document.querySelector('.chat-shell')!, { childList: true })
		})
		await page.getByRole('button', { name: '設定' }).click()
		await expect(page.locator('.settings-pane')).toBeVisible()
		const settingsStartTx = await page.evaluate(
			() => (window as unknown as { __settingsStartTx?: number }).__settingsStartTx ?? 0
		)
		expect(settingsStartTx).toBeGreaterThanOrEqual(viewport.width * 0.8)

		// Touch Events の左端スワイプで設定からチャンネル一覧へ戻る
		let sawSwipingClassOnLayer1 = false
		await performTouchEdgeSwipeRight(page, cdp, 8, 260, 8, 8, async () => {
			const cls = await page.locator('.chat-shell').getAttribute('class')
			if (cls?.includes('is-swiping-layer1')) {
				sawSwipingClassOnLayer1 = true
			}
		})
		expect(sawSwipingClassOnLayer1).toBe(true)
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-channels/)

		// 2. チャンネル → スレッドを開くときのスライドイン開始位置が右端 (100%) から始まること
		await page.getByRole('button', { name: /general/ }).click()
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-messages/)

		await page.evaluate(() => {
			;(window as unknown as { __threadStartTx?: number }).__threadStartTx = undefined
			const obs = new MutationObserver((mutations) => {
				for (const m of mutations) {
					for (const node of Array.from(m.addedNodes)) {
						if (node instanceof HTMLElement && node.classList.contains('thread-pane')) {
							const tr = getComputedStyle(node).transform
							const match = tr.match(/matrix\([^,]+,[^,]+,[^,]+,[^,]+,([^,]+),/)
							;(window as unknown as { __threadStartTx?: number }).__threadStartTx = match
								? parseFloat(match[1])
								: 0
							obs.disconnect()
						}
					}
				}
			})
			obs.observe(document.querySelector('.chat-shell')!, { childList: true })
		})
		await page.locator('#chat-msg-1700000080\\.000100 .thread-open-btn').click()
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-thread/)
		const threadStartTx = await page.evaluate(
			() => (window as unknown as { __threadStartTx?: number }).__threadStartTx ?? 0
		)
		expect(threadStartTx).toBeGreaterThanOrEqual(viewport.width * 0.8)

		// わずかなタッチスワイプでゆっくり放した時は戻らないこと
		await performTouchEdgeSwipeRight(page, cdp, 10, 48, 12, 25)
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-thread/)

		// 十分なタッチスワイプ中に指追従クラスが付与され、スレッド → チャンネルへ戻ること
		let sawSwipingClassOnThread = false
		await performTouchEdgeSwipeRight(page, cdp, 8, 260, 8, 8, async () => {
			const cls = await page.locator('.chat-shell').getAttribute('class')
			if (cls?.includes('is-swiping-thread')) {
				sawSwipingClassOnThread = true
			}
		})
		expect(sawSwipingClassOnThread).toBe(true)
		await expect(page.locator('.chat-shell')).toHaveClass(/mobile-messages/)
		await expect(page).toHaveURL(/\/channels\/C01GENERAL$/)
	})

	test('PC 幅でのスレッド切替後に × で閉じると前スレッドに戻らず、NOTIFICATION_CLICK で遷移できる', async ({
		page
	}) => {
		await page.setViewportSize({ width: 1280, height: 800 })
		await loginAsTestUser(page)

		await page.getByRole('button', { name: /general/ }).click()
		await expect(page.locator('#chat-msg-1700000080\\.000100')).toBeVisible()

		// スレッド T1 (#079) を開く
		await page.locator('#chat-msg-1700000079\\.000100 .thread-open-btn').click()
		await expect(page.locator('.thread-pane')).toBeVisible()
		await expect(page).toHaveURL(/\/channels\/C01GENERAL\/threads\/1700000079\.000100$/)

		// タイムラインから別スレッド T2 (#080) に切り替える
		await page.locator('#chat-msg-1700000080\\.000100 .thread-open-btn').click()
		await expect(page).toHaveURL(/\/channels\/C01GENERAL\/threads\/1700000080\.000100$/)

		// × ボタンで閉じたとき T1 に戻らず、スレッドペインが閉じてチャンネル URL になること
		await page.locator('.thread-header .desktop-only-btn').click()
		await expect(page.locator('.thread-pane')).toBeHidden()
		await expect(page).toHaveURL(/\/channels\/C01GENERAL$/)

		// Service Worker からの NOTIFICATION_CLICK postMessage を受信して該当スレッドへ遷移すること
		await page.evaluate(() => {
			navigator.serviceWorker.dispatchEvent(
				new MessageEvent('message', {
					data: { type: 'NOTIFICATION_CLICK', url: '/alerts/42' }
				})
			)
		})
		await expect(page).toHaveURL(/\/alerts\/42$/)
		await expect(page.locator('.thread-pane')).toBeVisible()
		await expect(page.getByText('スレッド返信 #25')).toBeVisible()
	})

	test('アプリが画面全体を覆い、スクロールできない場所のタッチで文書が動かない', async ({ page }) => {
		await loginAsTestUser(page)
		// チャンネル一覧 (中身が少なくスクロールできない) を表示した状態
		await expect(page.locator('.sidebar-scroll')).toBeVisible()

		const coverage = await page.evaluate(() => {
			const rect = document.getElementById('root')!.getBoundingClientRect()
			return {
				top: rect.top,
				bottom: rect.bottom,
				innerHeight: window.innerHeight,
				inlineAppHeight: document.documentElement.style.getPropertyValue('--app-height')
			}
		})
		// キーボードが閉じている間は高さを固定せず、画面の下端まで覆う
		expect(coverage.top).toBe(0)
		expect(Math.abs(coverage.bottom - coverage.innerHeight)).toBeLessThanOrEqual(1)
		expect(coverage.inlineAppHeight).toBe('')

		const dispatchMove = (selector: string, dy: number) =>
			page.evaluate(
				({ selector, dy }) => {
					const el = document.querySelector(selector)!
					const rect = el.getBoundingClientRect()
					const x = rect.left + rect.width / 2
					const y = rect.top + rect.height / 2
					const mk = (cy: number) =>
						new Touch({ identifier: 1, target: el, clientX: x, clientY: cy })
					el.dispatchEvent(
						new TouchEvent('touchstart', {
							bubbles: true,
							cancelable: true,
							touches: [mk(y)],
							changedTouches: [mk(y)]
						})
					)
					const move = new TouchEvent('touchmove', {
						bubbles: true,
						cancelable: true,
						touches: [mk(y + dy)],
						changedTouches: [mk(y + dy)]
					})
					el.dispatchEvent(move)
					el.dispatchEvent(
						new TouchEvent('touchend', { bubbles: true, cancelable: true, touches: [], changedTouches: [mk(y + dy)] })
					)
					return move.defaultPrevented
				},
				{ selector, dy }
			)

		// スクロールできないヘッダーと、中身の少ないチャンネル一覧では文書へ伝えない
		expect(await dispatchMove('.sidebar-header', -60)).toBe(true)
		expect(await dispatchMove('.sidebar-scroll', -60)).toBe(true)

		// スクロールできるメッセージ一覧では妨げない (一番下にいるので、上へ戻る向きに動かす)
		await page.getByRole('button', { name: /general/ }).click()
		await expect(page.locator('#chat-msg-1700000080\\.000100')).toBeVisible()
		expect(await dispatchMove('.chat-timeline', 60)).toBe(false)
	})
})
