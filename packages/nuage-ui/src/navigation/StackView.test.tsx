import { describe, expect, test } from 'vitest'
import type { Location } from 'react-router-dom'
import { nextStack, type Entry } from './StackView'

const loc = (pathname: string, key = pathname): Location => ({ pathname, search: '', hash: '', state: null, key })
const entry = (pathname: string, pushed = true): Entry => ({ key: `k:${pathname}`, location: loc(pathname), pushed })

describe('nextStack (URL の変化を、積む・戻る・入れ替えるに読み替える)', () => {
	const home = entry('/tabs/home', false)
	const channel = entry('/tabs/home/channels/C1')

	test('PUSH は画面を積み、動きを付ける', () => {
		const r = nextStack([home], loc('/tabs/home/channels/C1', 'n1'), 'PUSH', undefined, null, true)
		expect(r.entries.map((e) => e.location.pathname)).toEqual(['/tabs/home', '/tabs/home/channels/C1'])
		expect(r.entries[1].pushed).toBe(true)
		expect(r.transition).toMatchObject({ kind: 'push', revealKey: home.key })
	})

	test('動きを減らす設定では、積むだけで動きを付けない', () => {
		const r = nextStack([home], loc('/tabs/home/channels/C1'), 'PUSH', undefined, null, false)
		expect(r.entries).toHaveLength(2)
		expect(r.transition).toBeUndefined()
	})

	test('アプリの戻る (history.back) は下の画面へ戻り、手前の画面を引き出す動きを付ける', () => {
		const r = nextStack([home, channel], loc('/tabs/home', 'b1'), 'POP', undefined, 'animated', true)
		expect(r.entries.map((e) => e.location.pathname)).toEqual(['/tabs/home'])
		expect(r.entries[0].key).toBe(home.key)
		expect(r.leaving).toBe(channel)
		expect(r.transition).toMatchObject({ kind: 'pop', revealKey: home.key })
	})

	test('ブラウザ自身の戻る (Safari のスワイプ等) は動きを付けずに戻る (二重に動かない)', () => {
		const r = nextStack([home, channel], loc('/tabs/home', 'b2'), 'POP', undefined, null, true)
		expect(r.entries).toHaveLength(1)
		expect(r.leaving).toBeUndefined()
	})

	test('指で動かし終えた戻る (swiped) は動きを付けない', () => {
		const r = nextStack([home, channel], loc('/tabs/home', 'b3'), 'POP', undefined, 'swiped', true)
		expect(r.entries).toHaveLength(1)
		expect(r.transition).toBeUndefined()
	})

	test('直接開いた画面から親へ戻る (back-root) は、親を下に置いて手前を引き出す', () => {
		const thread = entry('/tabs/home/channels/C1/threads/1', false)
		const r = nextStack([thread], loc('/tabs/home/channels/C1', 'r1'), 'REPLACE', 'back-root', null, true)
		expect(r.entries.map((e) => e.location.pathname)).toEqual(['/tabs/home/channels/C1'])
		expect(r.leaving).toBe(thread)
		expect(r.transition?.kind).toBe('pop')
	})

	test('積んでいない場所への POP (進む・別の履歴) は、その画面だけにする', () => {
		const r = nextStack([home, channel], loc('/tabs/home/channels/C9'), 'POP', undefined, null, true)
		expect(r.entries.map((e) => e.location.pathname)).toEqual(['/tabs/home/channels/C9'])
	})

	test('同じタブをもう一度押す (tab-root) と、いちばん下の画面へ戻る', () => {
		const r = nextStack([home, channel], loc('/tabs/home', 't1'), 'PUSH', 'tab-root', null, true)
		expect(r.entries).toEqual([{ ...home, location: loc('/tabs/home', 't1') }])
	})

	test('タブの切り替えで、いちばん手前と同じ画面に来た時は積まない', () => {
		const r = nextStack([home, channel], loc('/tabs/home/channels/C1', 't2'), 'PUSH', 'tab', null, true)
		expect(r.entries).toHaveLength(2)
		expect(r.transition).toBeUndefined()
	})

	test('REPLACE はいちばん手前を入れ替える', () => {
		const r = nextStack([home, channel], loc('/tabs/home/channels/C2'), 'REPLACE', undefined, null, true)
		expect(r.entries.map((e) => e.location.pathname)).toEqual(['/tabs/home', '/tabs/home/channels/C2'])
	})
})
