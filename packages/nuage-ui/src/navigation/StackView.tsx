import {
	createContext,
	memo,
	useCallback,
	useContext,
	useLayoutEffect,
	useMemo,
	useRef,
	useState,
	type ReactNode,
	type PointerEvent as ReactPointerEvent
} from 'react'
import { useLocation, useNavigate, useNavigationType, useRoutes, type Location, type RouteObject } from 'react-router-dom'
import { useIsStandalone, usePrefersReducedMotion } from '../shell/media'

/**
 * location.state.nuageNav に載せる、移り変わりの種類である。
 * - back / back-swiped: 下の画面へ戻る (swiped は指で動かし終えているので動きを付けない)
 * - back-root / back-root-swiped: 下に画面が無い時に、親の画面へ戻る (直接開いた URL から戻る時)
 * - tab / tab-root: タブの切り替え (tab-root は同じタブをもう一度押して一番上へ戻る)
 */
export type NuageNav = 'back' | 'back-swiped' | 'back-root' | 'back-root-swiped' | 'tab' | 'tab-root'

interface NavState {
	nuageNav?: NuageNav
}

export interface Entry {
	key: string
	location: Location
	// この StackView で PUSH して積んだか (なら history.back() で下の画面へ戻れる)
	pushed: boolean
}

interface StackContextValue {
	depth: number
	canGoBack: boolean
	back: (opts?: { swiped?: boolean }) => void
}

interface StackEntryContextValue {
	isTop: boolean
	index: number
	location: Location
}

const StackContext = createContext<StackContextValue | null>(null)
const StackEntryContext = createContext<StackEntryContextValue | null>(null)

/** いちばん近い StackView の操作 (戻る) を返す。StackView の外では null。 */
export function useStack(): StackContextValue | null {
	return useContext(StackContext)
}

/**
 * 今の画面がいちばん手前に見えているか。StackView の外 (PC の横並びの画面など) では常に true。
 * 既読にする・新着を追いかける等、「見えている間だけ」の処理に使う。
 */
export function useIsTopPage(): boolean {
	return useContext(StackEntryContext)?.isTop ?? true
}

/** この画面が描いている location (積まれた画面は、それぞれ自分の location を持つ)。 */
export function usePageLocation(): Location {
	const entry = useContext(StackEntryContext)
	const location = useLocation()
	return entry?.location ?? location
}

const pathOf = (l: Pick<Location, 'pathname' | 'search'>) => l.pathname + l.search
const samePath = (a: Location, b: Location) => pathOf(a) === pathOf(b)

let keySeq = 0
const newKey = () => `nui-stack-${++keySeq}`

interface StackViewProps {
	/** この StackView が受け持つ URL の先頭 (例: /tabs/home)。ここから外れた URL では何もしない */
	basePath: string
	/** 画面の割り当て。パスは絶対パスで書く (例: /tabs/home/channels/:channelId) */
	routes: RouteObject[]
	/** 下に画面が無い時 (直接開いた URL) に戻る先。戻れないなら null */
	parentPath?: (pathname: string) => string | null
	/** 見えているタブか (見えていないタブは動きを付けない) */
	active?: boolean
	/** いちばん手前の画面と積まれた数が変わった時に呼ばれる (タブバーの出し入れ・タブの切り替え先に使う) */
	onTopChange?: (path: string, depth: number) => void
}

const EDGE_WIDTH = 24
const COMPLETE_PROGRESS = 0.35
const COMPLETE_VELOCITY = 0.5 // px/ms

/**
 * iOS のナビゲーションのように、画面を押し込んで積み、戻る時は手前の画面を右へ引き出す。
 * 積まれた下の画面は DOM に残す (戻った時にスクロール位置や入力が残る)。
 * ホーム画面 PWA では、左端からのスワイプで指についてきて戻れる
 * (Safari のタブではブラウザ自身のスワイプで戻るので、自前のスワイプは付けない)。
 */
export function StackView({ basePath, routes, parentPath, active = true, onTopChange }: StackViewProps) {
	const location = useLocation()
	const navType = useNavigationType()
	const navigate = useNavigate()
	const reducedMotion = usePrefersReducedMotion()
	const standalone = useIsStandalone()

	const inBase = useCallback(
		(pathname: string) => pathname === basePath || pathname.startsWith(basePath + '/'),
		[basePath]
	)

	const [entries, setEntries] = useState<Entry[]>(() =>
		inBase(location.pathname) ? [{ key: newKey(), location, pushed: false }] : []
	)
	const [leaving, setLeaving] = useState<Entry | null>(null)
	const [transition, setTransition] = useState<Transition | null>(null)
	const [dragging, setDragging] = useState(false)
	const pendingBackRef = useRef<'animated' | 'swiped' | null>(null)
	const handledKeyRef = useRef<string>(inBase(location.pathname) ? location.key : '')
	const pageEls = useRef(new Map<string, HTMLDivElement>())
	const animate = active && !reducedMotion

	const entriesRef = useRef(entries)
	entriesRef.current = entries

	// URL の変化を、積む・戻る・入れ替える に読み替える (描画前に済ませ、違う画面が一瞬見えないようにする)
	useLayoutEffect(() => {
		if (!inBase(location.pathname) || handledKeyRef.current === location.key) return
		handledKeyRef.current = location.key
		const nav = (location.state as NavState | null)?.nuageNav
		const pending = pendingBackRef.current
		pendingBackRef.current = null
		const prev = entriesRef.current
		const r = nextStack(prev, location, navType, nav, pending, animate)
		setEntries(r.entries)
		if (r.leaving) setLeaving(r.leaving)
		if (r.transition) setTransition(r.transition)
	}, [location, navType, inBase, animate])

	// 動きを付ける (Web Animations。終わったら片付ける)
	useLayoutEffect(() => {
		if (!transition) return
		const duration = 420
		const easing = 'cubic-bezier(0.32, 0.72, 0, 1)'
		const anims: Animation[] = []
		if (transition.kind === 'push' && transition.enterKey) {
			const enter = pageEls.current.get(transition.enterKey)
			const below = pageEls.current.get(transition.revealKey)
			if (enter) anims.push(enter.animate([{ transform: 'translateX(100%)' }, { transform: 'translateX(0)' }], { duration, easing }))
			if (below) anims.push(below.animate([{ transform: 'translateX(0)' }, { transform: 'translateX(-30%)' }], { duration, easing }))
		} else if (transition.kind === 'pop' && leaving) {
			const out = pageEls.current.get(leaving.key)
			const reveal = pageEls.current.get(transition.revealKey)
			const from = out?.style.transform || 'translateX(0)'
			if (out) {
				out.style.transform = ''
				anims.push(out.animate([{ transform: from }, { transform: 'translateX(100%)' }], { duration, easing, fill: 'forwards' }))
			}
			if (reveal) {
				const revealFrom = reveal.style.transform || 'translateX(-30%)'
				reveal.style.transform = ''
				anims.push(reveal.animate([{ transform: revealFrom }, { transform: 'translateX(0)' }], { duration, easing }))
			}
		}
		let cancelled = false
		Promise.all(anims.map((a) => a.finished.catch(() => undefined))).then(() => {
			if (cancelled) return
			setTransition(null)
			setLeaving(null)
		})
		return () => {
			cancelled = true
		}
	}, [transition, leaving])

	const top = entries[entries.length - 1]
	const depth = entries.length
	const fallbackParent = top ? (parentPath?.(top.location.pathname) ?? null) : null
	const canGoBack = depth > 1 || fallbackParent !== null

	useLayoutEffect(() => {
		if (top) onTopChange?.(pathOf(top.location), depth)
	}, [top, depth, onTopChange])

	const back = useCallback(
		(opts?: { swiped?: boolean }) => {
			const swiped = Boolean(opts?.swiped)
			const below = entries[entries.length - 2]
			const current = entries[entries.length - 1]
			if (below && current) {
				if (current.pushed) {
					pendingBackRef.current = swiped ? 'swiped' : 'animated'
					navigate(-1)
				} else {
					navigate(pathOf(below.location), { state: { nuageNav: swiped ? 'back-swiped' : 'back' } satisfies NavState })
				}
				return
			}
			if (fallbackParent) {
				navigate(fallbackParent, {
					replace: true,
					state: { nuageNav: swiped ? 'back-root-swiped' : 'back-root' } satisfies NavState
				})
			}
		},
		[entries, fallbackParent, navigate]
	)

	// ---- 左端からのスワイプで戻る (ホーム画面 PWA のみ) ----
	const dragRef = useRef<{ startX: number; startT: number; lastX: number; lastT: number; width: number } | null>(null)
	const belowKey = entries[entries.length - 2]?.key
	// 指の動きに合わせた位置の更新は、1 フレームに 1 回にまとめる
	const frameRef = useRef<number | null>(null)
	const pendingDxRef = useRef(0)
	// 指で戻し終えた後の片付け。URL が変わって手前の画面が外れてから行う
	// (先に片付けると、外れる前の 1 フレームだけ手前の画面が元の位置に戻って見え、カクつく)
	const swipeCleanupRef = useRef<(() => void) | null>(null)

	useLayoutEffect(() => {
		const cleanup = swipeCleanupRef.current
		if (!cleanup) return
		swipeCleanupRef.current = null
		cleanup()
	}, [top?.key])

	const setDragTransforms = (dx: number, width: number) => {
		const topEl = top ? pageEls.current.get(top.key) : undefined
		const belowEl = belowKey ? pageEls.current.get(belowKey) : undefined
		const progress = Math.min(Math.max(dx / width, 0), 1)
		if (topEl) topEl.style.transform = `translateX(${dx}px)`
		if (belowEl) belowEl.style.transform = `translateX(${-30 + 30 * progress}%)`
	}

	const onEdgeDown = (e: ReactPointerEvent<HTMLDivElement>) => {
		if (!canGoBack || transition) return
		e.currentTarget.setPointerCapture(e.pointerId)
		const width = e.currentTarget.parentElement?.clientWidth ?? window.innerWidth
		dragRef.current = { startX: e.clientX, startT: e.timeStamp, lastX: e.clientX, lastT: e.timeStamp, width }
		setDragging(true)
	}
	const onEdgeMove = (e: ReactPointerEvent<HTMLDivElement>) => {
		const d = dragRef.current
		if (!d) return
		d.lastX = e.clientX
		d.lastT = e.timeStamp
		pendingDxRef.current = Math.max(0, e.clientX - d.startX)
		if (frameRef.current !== null) return
		frameRef.current = requestAnimationFrame(() => {
			frameRef.current = null
			if (dragRef.current) setDragTransforms(pendingDxRef.current, dragRef.current.width)
		})
	}
	const onEdgeUp = (e: ReactPointerEvent<HTMLDivElement>) => {
		const d = dragRef.current
		dragRef.current = null
		if (!d) return
		if (frameRef.current !== null) {
			cancelAnimationFrame(frameRef.current)
			frameRef.current = null
		}
		const dx = Math.max(0, e.clientX - d.startX)
		const velocity = (e.clientX - d.lastX) / Math.max(1, e.timeStamp - d.lastT) || dx / Math.max(1, e.timeStamp - d.startT)
		const complete = dx / d.width > COMPLETE_PROGRESS || velocity > COMPLETE_VELOCITY
		const topEl = top ? pageEls.current.get(top.key) : undefined
		const belowEl = belowKey ? pageEls.current.get(belowKey) : undefined
		const duration = 260
		const easing = 'cubic-bezier(0.32, 0.72, 0, 1)'
		const finish = () => {
			setDragging(false)
			if (topEl) topEl.style.transform = ''
			if (belowEl) belowEl.style.transform = ''
		}
		if (complete) {
			const anims = [
				topEl?.animate([{ transform: `translateX(${dx}px)` }, { transform: 'translateX(100%)' }], { duration, easing, fill: 'forwards' }),
				belowEl?.animate([{ transform: belowEl.style.transform || 'translateX(-30%)' }, { transform: 'translateX(0)' }], {
					duration,
					easing,
					fill: 'forwards'
				})
			].filter((a): a is Animation => Boolean(a))
			Promise.all(anims.map((a) => a.finished.catch(() => undefined))).then(() => {
				const cleanup = () => {
					anims.forEach((a) => a.cancel())
					finish()
				}
				swipeCleanupRef.current = cleanup
				back({ swiped: true })
				// 戻れなかった時 (URL が変わらなかった時) の保険
				window.setTimeout(() => {
					if (swipeCleanupRef.current === cleanup) {
						swipeCleanupRef.current = null
						cleanup()
					}
				}, 1000)
			})
		} else {
			const anims = [
				topEl?.animate([{ transform: `translateX(${dx}px)` }, { transform: 'translateX(0)' }], { duration, easing }),
				belowEl?.animate([{ transform: belowEl.style.transform || 'translateX(-30%)' }, { transform: 'translateX(-30%)' }], {
					duration,
					easing
				})
			].filter((a): a is Animation => Boolean(a))
			if (topEl) topEl.style.transform = ''
			Promise.all(anims.map((a) => a.finished.catch(() => undefined))).then(finish)
		}
	}

	const ctx = useMemo<StackContextValue>(() => ({ depth, canGoBack, back }), [depth, canGoBack, back])

	const rendered = leaving ? [...entries, leaving] : entries
	const topIndex = entries.length - 1
	return (
		<StackContext.Provider value={ctx}>
			<div className="nui-stack" data-moving={dragging || transition !== null ? 'true' : 'false'}>
				{rendered.map((entry, i) => {
					const isLeaving = entry === leaving
					const isTop = !isLeaving && i === topIndex
					const visible =
						isTop || isLeaving || entry.key === transition?.revealKey || (dragging && entry.key === belowKey)
					return (
						<div
							key={entry.key}
							ref={(el) => {
								if (el) pageEls.current.set(entry.key, el)
								else pageEls.current.delete(entry.key)
							}}
							className="nui-stack-page"
							data-visible={visible ? 'true' : 'false'}
							aria-hidden={!isTop}
						>
							<StackPage entry={entry} index={i} isTop={isTop} routes={routes} />
						</div>
					)
				})}
				{standalone && canGoBack && !transition && (
					<div
						className="nui-stack-edge"
						onPointerDown={onEdgeDown}
						onPointerMove={onEdgeMove}
						onPointerUp={onEdgeUp}
						onPointerCancel={onEdgeUp}
						style={{ width: EDGE_WIDTH }}
						aria-hidden="true"
					/>
				)}
			</div>
		</StackContext.Provider>
	)
}

type Transition = { kind: 'push' | 'pop'; enterKey?: string; revealKey: string }

/**
 * 今の画面の並び (prev) と新しい URL から、次の並びと付ける動きを決める。
 * 副作用を持たない関数にしてあり、テストで直接確かめる。
 */
export function nextStack(
	prev: Entry[],
	location: Location,
	navType: 'POP' | 'PUSH' | 'REPLACE',
	nav: NuageNav | undefined,
	pending: 'animated' | 'swiped' | null,
	animate: boolean
): { entries: Entry[]; leaving?: Entry; transition?: Transition } {
	const top = prev[prev.length - 1]
	const findBelow = () => {
		for (let i = prev.length - 2; i >= 0; i--) if (samePath(prev[i].location, location)) return i
		return -1
	}

	if (navType === 'POP' || nav === 'back' || nav === 'back-swiped') {
		const idx = findBelow()
		if (idx >= 0) {
			const next = prev.slice(0, idx + 1)
			next[idx] = { ...next[idx], location }
			const withMotion = animate && (navType === 'POP' ? pending === 'animated' : nav === 'back')
			return withMotion && top
				? { entries: next, leaving: top, transition: { kind: 'pop', revealKey: next[idx].key } }
				: { entries: next }
		}
		if (top && samePath(top.location, location)) return { entries: [...prev.slice(0, -1), { ...top, location }] }
		return { entries: [{ key: newKey(), location, pushed: false }] }
	}
	if (nav === 'back-root' || nav === 'back-root-swiped') {
		const root = { key: newKey(), location, pushed: false }
		return nav === 'back-root' && animate && top
			? { entries: [root], leaving: top, transition: { kind: 'pop', revealKey: root.key } }
			: { entries: [root] }
	}
	if (nav === 'tab-root') {
		if (prev[0] && samePath(prev[0].location, location)) return { entries: [{ ...prev[0], location }] }
		return { entries: [{ key: newKey(), location, pushed: false }] }
	}
	if (top && samePath(top.location, location)) return { entries: [...prev.slice(0, -1), { ...top, location }] }
	if (navType === 'REPLACE' || nav === 'tab') {
		return { entries: [...prev.slice(0, -1), { key: newKey(), location, pushed: top?.pushed ?? false }] }
	}
	// PUSH: 新しい画面を積む
	const entry = { key: newKey(), location, pushed: true }
	return animate && top
		? { entries: [...prev, entry], transition: { kind: 'push', enterKey: entry.key, revealKey: top.key } }
		: { entries: [...prev, entry] }
}

const StackPage = memo(function StackPage({
	entry,
	index,
	isTop,
	routes
}: {
	entry: Entry
	index: number
	isTop: boolean
	routes: RouteObject[]
}) {
	const value = useMemo(() => ({ isTop, index, location: entry.location }), [isTop, index, entry.location])
	return (
		<StackEntryContext.Provider value={value}>
			<EntryRoutes routes={routes} location={entry.location} />
		</StackEntryContext.Provider>
	)
})

function EntryRoutes({ routes, location }: { routes: RouteObject[]; location: Location }): ReactNode {
	return useRoutes(routes, location)
}
