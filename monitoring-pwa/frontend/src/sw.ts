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
		const title = data.title || '通知'
		const options = {
			body: data.body || '',
			icon: '/icons/icon-192.png',
			badge: '/icons/icon-192.png',
			data: {
				url: data.url || '/'
			},
			tag: 'monitoring-alert',
			renotify: true
		}

		event.waitUntil(
			self.registration.showNotification(title, options)
		)
	} catch (err) {
		console.error('Failed to parse push payload', err)
		event.waitUntil(
			self.registration.showNotification('Nuage Monitor', {
				body: event.data.text(),
				icon: '/icons/icon-192.png'
			})
		)
	}
})

// 通知クリック時のイベントハンドラである。指定された URL を開く、またはフォーカスする。
self.addEventListener('notificationclick', (event) => {
	event.notification.close()

	const targetUrl = event.notification.data?.url || '/'
	const absoluteTargetUrl = new URL(targetUrl, self.location.origin).href

	event.waitUntil(
		self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
			// すでに同じオリジンのウィンドウが開いているかチェックする
			for (const client of clientList) {
				const clientUrl = new URL(client.url)
				if (clientUrl.origin === self.location.origin && 'focus' in client) {
					// 既存のウィンドウにフォーカスを当て、そのURLを目的のものにナビゲートする
					if ('navigate' in client) {
						client.navigate(absoluteTargetUrl)
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
