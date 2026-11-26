/**
 * 実機 (iOS Safari・ホーム画面 PWA) で、画面の高さとスクロールの値を確かめるための一時的な計測表示である。
 * URL に `?debug=viewport` を付けて開くと有効になり、`?debug=off` で消える (localStorage に覚える)。
 * iOS のホーム画面 PWA は Safari と localStorage を共有せず URL も打てないため、
 * 「Nuage Notify」の大見出しを 2 秒以内に 5 回タップするか、設定画面のスイッチでも切り替えられる。
 * 原因が分かったら取り除く。
 */

const STORAGE_KEY = 'nuage-debug-viewport'

function describe(el: EventTarget | null): string {
	if (!(el instanceof Element)) {
		return el === document ? 'document' : el === window ? 'window' : String(el)
	}
	const id = el.id ? `#${el.id}` : ''
	const cls =
		typeof el.className === 'string' && el.className
			? '.' + el.className.trim().split(/\s+/).slice(0, 2).join('.')
			: ''
	return `${el.tagName.toLowerCase()}${id}${cls}`
}

export function isViewportDebugEnabled(): boolean {
	try {
		return localStorage.getItem(STORAGE_KEY) === '1'
	} catch {
		return false
	}
}

// setViewportDebugEnabled は計測表示の有無を覚えて、読み込み直す。
export function setViewportDebugEnabled(enabled: boolean): void {
	try {
		if (enabled) localStorage.setItem(STORAGE_KEY, '1')
		else localStorage.removeItem(STORAGE_KEY)
	} catch {
		return
	}
	window.location.reload()
}

function isEnabled(): boolean {
	const param = new URLSearchParams(window.location.search).get('debug')
	try {
		if (param === 'viewport') localStorage.setItem(STORAGE_KEY, '1')
		if (param === 'off') localStorage.removeItem(STORAGE_KEY)
		return localStorage.getItem(STORAGE_KEY) === '1'
	} catch {
		return param === 'viewport'
	}
}

function installTapToggle(): void {
	let count = 0
	let firstAt = 0
	document.addEventListener(
		'click',
		(e) => {
			const target = e.target instanceof Element ? e.target : null
			if (!target?.closest('.app-title-tap')) return
			const now = Date.now()
			if (now - firstAt > 2000) {
				count = 0
				firstAt = now
			}
			count += 1
			if (count < 5) return
			count = 0
			try {
				if (localStorage.getItem(STORAGE_KEY) === '1') localStorage.removeItem(STORAGE_KEY)
				else localStorage.setItem(STORAGE_KEY, '1')
			} catch {
				return
			}
			window.location.reload()
		},
		{ capture: true }
	)
}

export function installViewportDebug(): void {
	installTapToggle()
	if (!isEnabled()) return

	const panel = document.createElement('pre')
	panel.setAttribute('aria-hidden', 'true')
	panel.style.cssText = [
		'position:fixed',
		'left:4px',
		'top:calc(env(safe-area-inset-top, 0px) + 60px)',
		'z-index:2147483647',
		'margin:0',
		'padding:6px 8px',
		'max-width:calc(100vw - 8px)',
		'font:10px/1.35 ui-monospace,Menlo,monospace',
		'color:#0f0',
		'background:rgba(0,0,0,0.82)',
		'border:1px solid #0f0',
		'border-radius:4px',
		'pointer-events:none',
		'white-space:pre-wrap',
		'user-select:none'
	].join(';')

	// safe-area の値は env() を一度要素に当てて読み取る
	const probe = document.createElement('div')
	probe.style.cssText =
		'position:fixed;visibility:hidden;pointer-events:none;' +
		'padding-top:env(safe-area-inset-top,0px);padding-bottom:env(safe-area-inset-bottom,0px);' +
		'top:0;left:0;width:0;height:0'
	const sizers = ['100vh', '100svh', '100lvh', '100dvh'].map((h) => {
		const el = document.createElement('div')
		el.style.cssText = `position:absolute;visibility:hidden;pointer-events:none;top:0;left:0;width:0;height:${h}`
		return [h, el] as const
	})

	let lastScroll = '-'
	let lastTouch = '-'
	const scrolledElements = new Map<string, number>()

	document.addEventListener(
		'scroll',
		(e) => {
			const name = describe(e.target)
			const t = e.target
			const top =
				t === document
					? (document.scrollingElement?.scrollTop ?? 0)
					: t instanceof Element
						? t.scrollTop
						: 0
			scrolledElements.set(name, Math.round(top))
			lastScroll = `${name} top=${Math.round(top)}`
			render()
		},
		{ capture: true, passive: true }
	)

	// useDocumentScrollLock の判断 (preventDefault したか) を、全ハンドラの後で読む
	window.addEventListener(
		'touchmove',
		(e) => {
			lastTouch = `${describe(e.target)} prevented=${e.defaultPrevented} cancelable=${e.cancelable}`
			render()
		},
		{ passive: true }
	)

	const render = () => {
		const vv = window.visualViewport
		const root = document.getElementById('root')
		const rr = root?.getBoundingClientRect()
		const br = document.body.getBoundingClientRect()
		const se = document.scrollingElement
		const ps = getComputedStyle(probe)
		const standalone =
			window.matchMedia('(display-mode: standalone)').matches ||
			(navigator as Navigator & { standalone?: boolean }).standalone === true
		const others = [...scrolledElements.entries()]
			.filter(([, top]) => top !== 0)
			.map(([n, top]) => `  ${n}=${top}`)
			.join('\n')
		panel.textContent = [
			`standalone=${standalone} screen=${screen.width}x${screen.height}`,
			`inner=${window.innerWidth}x${window.innerHeight} clientH=${document.documentElement.clientHeight}`,
			`vv h=${vv ? Math.round(vv.height) : '-'} top=${vv ? Math.round(vv.offsetTop) : '-'} pageTop=${vv ? Math.round(vv.pageTop) : '-'} scale=${vv ? vv.scale.toFixed(2) : '-'}`,
			`scrollY=${Math.round(window.scrollY)} se=${se ? describe(se) : '-'} seTop=${se ? Math.round(se.scrollTop) : '-'} seH=${se ? `${se.scrollHeight}/${se.clientHeight}` : '-'}`,
			`html h=${Math.round(document.documentElement.getBoundingClientRect().height)} body top=${Math.round(br.top)} h=${Math.round(br.height)} pos=${getComputedStyle(document.body).position}`,
			`#root top=${rr ? Math.round(rr.top) : '-'} bottom=${rr ? Math.round(rr.bottom) : '-'} h=${rr ? Math.round(rr.height) : '-'}`,
			`safe top=${ps.paddingTop} bottom=${ps.paddingBottom}`,
			sizers.map(([h, el]) => `${h}=${Math.round(el.getBoundingClientRect().height)}`).join(' '),
			`--app-height=${document.documentElement.style.getPropertyValue('--app-height') || '(css)'} kb=${document.documentElement.dataset.keyboardOpen ?? 'no'}`,
			`last scroll: ${lastScroll}`,
			others ? `scrolled now:\n${others}` : 'scrolled now: (none)',
			`last touchmove: ${lastTouch}`
		].join('\n')
	}

	const mount = () => {
		document.body.appendChild(probe)
		for (const [, el] of sizers) document.body.appendChild(el)
		document.body.appendChild(panel)
		render()
		window.visualViewport?.addEventListener('resize', render)
		window.visualViewport?.addEventListener('scroll', render)
		window.addEventListener('resize', render)
		window.setInterval(render, 500)
	}

	if (document.body) mount()
	else window.addEventListener('DOMContentLoaded', mount, { once: true })
}
