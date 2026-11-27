import { describe, expect, test } from 'bun:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router-dom'
import ChannelNav from '../src/components/ChannelNav'

const BOT = { id: 'B01BOT', user_id: 'U01BOT', name: 'nuage-bot', online: true }

function renderNav(props: Partial<Parameters<typeof ChannelNav>[0]>) {
	return renderToStaticMarkup(
		createElement(
			MemoryRouter,
			null,
			createElement(ChannelNav, {
				channels: [],
				bot: BOT,
				channelsLoading: false,
				query: '',
				onQueryChange: () => {},
				searchResults: null,
				searching: false,
				searchError: null,
				channelLinkFor: (id) => `/tabs/home/channels/${id}`,
				threadLinkFor: (cid, ts) => `/tabs/home/channels/${cid}/threads/${ts}`,
				...props
			})
		)
	)
}

describe('ChannelNav のローディング・空表示', () => {
	test('channelsLoading 中は「チャンネルはまだない」「DM はまだない」を出さずローディング表示になる', () => {
		const html = renderNav({ channels: [], channelsLoading: true })
		expect(html).not.toContain('チャンネルはまだない')
		expect(html).not.toContain('DM はまだない')
		expect(html).toContain('nui-spinner')
		expect(html).toContain('nui-skeleton')
	})

	test('channelsLoading 完了後で 0 件のときだけ空メッセージを出す', () => {
		const html = renderNav({ channels: [], channelsLoading: false })
		expect(html).toContain('チャンネルはまだない')
		expect(html).toContain('DM はまだない')
		expect(html).not.toContain('読み込み中…')
	})

	test('検索入力直後 (デバウンス中の結果待ち) でも検索中ローディングが出る', () => {
		const html = renderNav({
			query: 'alert',
			searching: true,
			searchResults: null
		})
		expect(html).toContain('検索結果')
		expect(html).toContain('nui-spinner')
		expect(html).toContain('nui-skeleton')
		expect(html).not.toContain('見つからなかった')
	})
})
