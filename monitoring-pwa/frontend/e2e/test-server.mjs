import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const distDir = path.resolve(__dirname, '../dist')
const port = Number(process.env.E2E_PORT || 4175)

const mimeTypes = {
	'.html': 'text/html; charset=utf-8',
	'.js': 'application/javascript; charset=utf-8',
	'.css': 'text/css; charset=utf-8',
	'.json': 'application/json; charset=utf-8',
	'.png': 'image/png',
	'.ico': 'image/x-icon',
	'.svg': 'image/svg+xml',
	'.webmanifest': 'application/manifest+json'
}

function makeMessages(startIdx, endIdx, channelId = 'C01GENERAL') {
	const list = []
	for (let i = startIdx; i <= endIdx; i++) {
		const tsNum = 1700000000 + i
		const ts = `${tsNum}.000100`
		let text = `メッセージ #${String(i).padStart(3, '0')}: クラスタ監視ログのテスト本文である。`
		if (i === 78) {
			text = 'スクリーンショット確認:\n\n![グラフ画像](https://mock-assets.local/graph.png)'
		} else if (i === 79) {
			text = '```sh\nkubectl get pods -n monitoring\n```'
		}
		list.push({
			id: i,
			channel_id: channelId,
			ts,
			user_id: i % 3 === 0 ? 'U01TEST' : undefined,
			bot_id: i % 3 === 0 ? undefined : 'B01BOT',
			username: i % 3 === 0 ? 'Admin' : 'nuage-bot',
			text,
			level: i === 80 ? 'error' : 'info',
			reply_count: i === 80 ? 25 : 0,
			created_at: new Date(tsNum * 1000).toISOString(),
			updated_at: new Date(tsNum * 1000).toISOString()
		})
	}
	return list
}

const allGeneralMessages = makeMessages(1, 80, 'C01GENERAL')
const dmMessages = makeMessages(1, 12, 'D01BOT')

function makeThreadReplies(parentTs, channelId = 'C01GENERAL') {
	const replies = []
	for (let i = 1; i <= 25; i++) {
		const tsNum = 1700001000 + i
		replies.push({
			id: 1000 + i,
			channel_id: channelId,
			ts: `${tsNum}.000100`,
			thread_ts: parentTs,
			bot_id: 'B01BOT',
			username: 'nuage-bot',
			text: `スレッド返信 #${String(i).padStart(2, '0')}: 詳細な確認結果`,
			created_at: new Date(tsNum * 1000).toISOString(),
			updated_at: new Date(tsNum * 1000).toISOString()
		})
	}
	return replies
}

const sseClients = new Set()

function sendJson(res, status, data, headers = {}) {
	res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', ...headers })
	res.end(JSON.stringify(data))
}

function readBody(req) {
	return new Promise((resolve) => {
		let raw = ''
		req.on('data', (chunk) => {
			raw += chunk
		})
		req.on('end', () => resolve(raw))
	})
}

function isAuthed(req) {
	const cookie = req.headers.cookie || ''
	return cookie.includes('e2e_auth=1')
}

const server = http.createServer(async (req, res) => {
	const url = new URL(req.url || '/', `http://127.0.0.1:${port}`)
	const pathname = url.pathname

	if (pathname === '/__test/sse' && req.method === 'POST') {
		const raw = await readBody(req)
		const parsed = JSON.parse(raw || '{}')
		const eventName = parsed.event || 'message_created'
		const payload = JSON.stringify(parsed.data || {})
		for (const client of sseClients) {
			client.write(`event: ${eventName}\ndata: ${payload}\n\n`)
		}
		sendJson(res, 200, { sent: sseClients.size })
		return
	}

	if (pathname === '/api/auth/login' && req.method === 'POST') {
		const raw = await readBody(req)
		const parsed = JSON.parse(raw || '{}')
		if (!parsed.passphrase || parsed.passphrase === 'wrong') {
			sendJson(res, 401, { error: 'invalid passphrase' })
			return
		}
		sendJson(
			res,
			200,
			{ ok: true },
			{ 'Set-Cookie': 'e2e_auth=1; Path=/; SameSite=Lax' }
		)
		return
	}

	if (pathname === '/api/auth/logout' && req.method === 'POST') {
		sendJson(
			res,
			200,
			{ ok: true },
			{ 'Set-Cookie': 'e2e_auth=; Path=/; Max-Age=0' }
		)
		return
	}

	if (pathname.startsWith('/api/')) {
		if (!isAuthed(req)) {
			sendJson(res, 401, { error: 'unauthorized' })
			return
		}

		if (pathname === '/api/bootstrap') {
			sendJson(res, 200, {
				user: { id: 'U01TEST', name: 'admin', display_name: 'Admin' },
				bot: { id: 'B01BOT', user_id: 'U01BOT', name: 'nuage-bot', online: true },
				channels: [
					{
						id: 'C01GENERAL',
						name: 'general',
						is_channel: true,
						is_im: false,
						topic: 'クラスタ全体通知',
						created_at: '2026-11-01T00:00:00Z',
						unread_count: 2
					},
					{
						id: 'D01BOT',
						name: 'nuage-bot',
						is_channel: false,
						is_im: true,
						dm_user_id: 'U01TEST',
						created_at: '2026-11-01T00:00:00Z',
						unread_count: 0
					}
				]
			})
			return
		}

		if (pathname === '/api/events') {
			res.writeHead(200, {
				'Content-Type': 'text/event-stream',
				'Cache-Control': 'no-cache',
				Connection: 'keep-alive'
			})
			res.write(': connected\n\n')
			sseClients.add(res)
			req.on('close', () => {
				sseClients.delete(res)
			})
			return
		}

		const msgMatch = pathname.match(/^\/api\/channels\/([^/]+)\/messages$/)
		if (msgMatch) {
			const channelId = decodeURIComponent(msgMatch[1])
			if (req.method === 'POST') {
				const raw = await readBody(req)
				const parsed = JSON.parse(raw || '{}')
				const nowTs = `${Math.floor(Date.now() / 1000)}.000500`
				const created = {
					id: 9999,
					channel_id: channelId,
					ts: nowTs,
					thread_ts: parsed.thread_ts || undefined,
					user_id: 'U01TEST',
					username: 'Admin',
					text: parsed.text || '',
					created_at: new Date().toISOString(),
					updated_at: new Date().toISOString()
				}
				sendJson(res, 200, { message: created, bot_online: true, delivered_to_bot: true })
				return
			}

			const before = url.searchParams.get('before')
			if (channelId === 'D01BOT') {
				sendJson(res, 200, { messages: before ? [] : dmMessages, has_more: false })
				return
			}
			if (before) {
				const older = allGeneralMessages.filter((m) => m.ts < before).slice(-30)
				sendJson(res, 200, { messages: older, has_more: false })
				return
			}
			sendJson(res, 200, {
				messages: allGeneralMessages.slice(-40),
				has_more: true
			})
			return
		}

		const readMatch = pathname.match(/^\/api\/channels\/([^/]+)\/read$/)
		if (readMatch && req.method === 'POST') {
			sendJson(res, 200, { ok: true })
			return
		}

		const threadMatch = pathname.match(/^\/api\/channels\/([^/]+)\/threads\/([^/]+)$/)
		if (threadMatch) {
			const channelId = decodeURIComponent(threadMatch[1])
			const ts = decodeURIComponent(threadMatch[2])
			const parent =
				allGeneralMessages.find((m) => m.ts === ts) || allGeneralMessages[allGeneralMessages.length - 1]
			sendJson(res, 200, {
				parent,
				replies: makeThreadReplies(parent.ts, channelId)
			})
			return
		}

		const dlMatch = pathname.match(/^\/api\/deeplink\/(alert|notification)\/(\d+)$/)
		if (dlMatch) {
			sendJson(res, 200, {
				channel_id: 'C01GENERAL',
				ts: allGeneralMessages[allGeneralMessages.length - 1].ts
			})
			return
		}

		if (pathname === '/api/search') {
			sendJson(res, 200, [
				{
					channel_id: 'C01GENERAL',
					channel_name: 'general',
					is_im: false,
					message: allGeneralMessages[allGeneralMessages.length - 1]
				}
			])
			return
		}

		sendJson(res, 404, { error: 'not found' })
		return
	}

	// 静的アセット配信 (SPA フォールバック付き)
	let filePath = path.join(distDir, pathname === '/' ? 'index.html' : pathname)
	if (!filePath.startsWith(distDir) || !fs.existsSync(filePath) || fs.statSync(filePath).isDirectory()) {
		filePath = path.join(distDir, 'index.html')
	}
	const ext = path.extname(filePath)
	res.writeHead(200, { 'Content-Type': mimeTypes[ext] || 'application/octet-stream' })
	fs.createReadStream(filePath).pipe(res)
})

server.listen(port, '127.0.0.1', () => {
	console.log(`E2E test server listening on http://127.0.0.1:${port}`)
})
