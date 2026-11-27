# @nuage/ui

Nuage の UI 規約 ([nuage-workspace/docs/ui-design.md](https://github.com/k-wa-wa/nuage-workspace/blob/master/docs/ui-design.md)) に沿った UI ライブラリである。
対象は **PC・タブレットのブラウザ** と **iPhone の PWA (ホーム画面に追加して開くもの)** に絞る。

今は nuage-monitoring-stack の中に置き、monitoring-pwa (Nuage Notify) が使う。後で別のリポジトリに切り出す前提で、アプリのコードは import しない。

## 使い方

```ts
import '@nuage/ui/styles.css' // トークン・土台・部品の CSS (一度だけ)
import { AppShell, TabbedStacks, Page, ListSection, ListItem, Button } from '@nuage/ui'
```

- 書体 (IBM Plex Sans / JP / Mono) はアプリ側で `@fontsource/*` を読み込む。
- アイコンは `lucide-react`、画面の割り当ては `react-router-dom` 6 を使う (peerDependencies)。

## 中身

| 分類 | もの |
|---|---|
| トークン・土台 | 規約の `:root` 変数 (ダーク専用) と、規約のガードレール (タップの光を消す・`:focus-visible`・`@media (hover: hover)`・押せる要素の `user-select: none`・スマホの入力欄 16px)。規約に無い値は `tokens.css` の「拡張」に分けて置く |
| 外枠 | `AppShell`: 画面全体を動かさない (html/body の固定と、スクロールできない場所のタッチを止める)、キーボードが開いている間は見えている高さに合わせる |
| 画面の移り変わり | `StackView` (押し込んで積み、戻る。ホーム画面 PWA では左端スワイプで指についてきて戻る)、`TabbedStacks` (タブごとに履歴を持つ。2 枚目以降はタブバーを隠す)、`SplitView` (PC・タブレットの横並び)、`BackButton` |
| 部品 | `Page` / `Toolbar` (ガラス)、`Button` / `IconButton`、`Badge` / `Chip`、`Card`、`ListSection` / `ListItem`、`TextField` / `TextArea` (入力に合わせて伸びる) / `SearchField`、`Segmented`、`Toggle`、`Modal` (スマホ幅では下から出るシート)、`ToastProvider` / `useToast`、`Spinner` / `EmptyState` |

押せる部品はすべて、押した瞬間に沈み、離すと弾んで戻る (規約 2.5 のモーション)。

## iPhone の PWA で確かめたこと

アプリの `index.html` は次を前提にする。

- `<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover, interactive-widget=resizes-content">`
- `<meta name="apple-mobile-web-app-status-bar-style" content="black">`
  - `black-translucent` は、ホーム画面 PWA で画面がステータスバー分 (59pt) 縮んだまま最上端に置かれ、下に描けない帯が残る (iPhone 実機で `screen=852` に対し `innerHeight=793`)。
- Safari のタブでは、ブラウザ自身のスワイプで戻るので、自前の左端スワイプは付けない (二重に動くため)。

## 開発

リポジトリ直下が bun の workspace である。

```sh
bun install                 # リポジトリ直下で
cd packages/nuage-ui
bun run typecheck
bun run test                # vitest (部品と、画面の積み重ねの決め方)
bun run storybook           # 部品の一覧 (http://localhost:6006)
```
