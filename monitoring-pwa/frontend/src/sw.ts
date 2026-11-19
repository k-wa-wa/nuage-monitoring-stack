/// <reference lib="webworker" />
import { precacheAndRoute } from 'workbox-precaching'

declare let self: ServiceWorkerGlobalScope

// ビルド時に VitePWA がキャッシュ対象アセットを注入する
precacheAndRoute(self.__WB_MANIFEST)

// プッシュ通知の受信ハンドラである。
self.addEventListener('push', (event) => {
	if (!event.data) {
		return
	}

	try {
		const data = event.data.json()
		const title = data.title || 'Nuage Notify'
		const options: NotificationOptions & { renotify?: boolean } = {
			body: data.body || '',
			icon: '/icons/icon-192.png',
			badge: '/icons/icon-192.png',
			data: {
				url: data.url || '/',
				target_url: data.target_url || '',
				direct_open: Boolean(data.direct_open)
			},
			tag: data.tag || 'monitoring-alert',
			renotify: true
		}

		event.waitUntil(
			self.registration.showNotification(title, options)
		)
	} catch (err) {
		console.error('Failed to parse push payload', err)
		event.waitUntil(
			self.registration.showNotification('Nuage Notify', {
				body: event.data.text(),
				icon: '/icons/icon-192.png'
			})
		)
	}
})

// 外部の http/https URL かどうかを判定する。
function isExternalHttpUrl(rawUrl: string): boolean {
	if (!rawUrl || (!rawUrl.startsWith('http://') && !rawUrl.startsWith('https://'))) {
		return false
	}
	try {
		return new URL(rawUrl).origin !== self.location.origin
	} catch {
		return false
	}
}

// 通知クリック時のイベントハンドラである。
// 標準では PWA 内の通知詳細を開き、direct_open 指定時または外部 URL 直接指定時は外部アプリを開く。
self.addEventListener('notificationclick', (event) => {
	event.notification.close()

	const data = (event.notification.data || {}) as {
		url?: string
		target_url?: string
		direct_open?: boolean
	}
	const directOpen = Boolean(data.direct_open)
	const externalTarget = typeof data.target_url === 'string' ? data.target_url : ''
	const rawUrl = typeof data.url === 'string' && data.url ? data.url : '/'

	if (directOpen && isExternalHttpUrl(externalTarget)) {
		event.waitUntil(self.clients.openWindow(externalTarget))
		return
	}
	if (isExternalHttpUrl(rawUrl)) {
		event.waitUntil(self.clients.openWindow(rawUrl))
		return
	}

	const absoluteTargetUrl = new URL(rawUrl, self.location.origin).href
	const parsedTarget = new URL(absoluteTargetUrl)
	const targetPath = `${parsedTarget.pathname}${parsedTarget.search}${parsedTarget.hash}`

	event.waitUntil(
		self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(async (clientList) => {
			// すでに同じオリジンのウィンドウが開いているかチェックする
			for (const client of clientList) {
				const clientUrl = new URL(client.url)
				if (clientUrl.origin === self.location.origin && 'focus' in client) {
					client.postMessage({ type: 'NOTIFICATION_CLICK', url: targetPath })
					if (client.url !== absoluteTargetUrl && 'navigate' in client) {
						try {
							await client.navigate(absoluteTargetUrl)
						} catch {
							// 一部環境で navigate が拒否された場合は postMessage にフォールバックする
						}
					}
					return client.focus()
				}
			}
			// なければ新しく開く
			if (self.clients.openWindow) {
				return self.clients.openWindow(absoluteTargetUrl)
			}
		})
	)
})
