// @nuage/ui: Nuage の UI 規約 (nuage-workspace/docs/ui-design.md) に沿った UI ライブラリ。
// 対象は PC・タブレットのブラウザと iPhone の PWA。CSS は '@nuage/ui/styles.css' を一度だけ読み込む。

export { AppShell } from './shell/AppShell'
export { useMediaQuery, useIsWide, usePrefersReducedMotion, useIsStandalone, WIDE_QUERY } from './shell/media'

export { StackView, useStack, useIsTopPage, usePageLocation, type NuageNav } from './navigation/StackView'
export { TabbedStacks, type TabDefinition } from './navigation/TabbedStacks'
export { TabBar, type TabBarItem } from './navigation/TabBar'
export { BackButton } from './navigation/BackButton'
export { SplitView } from './navigation/SplitView'

export { Page } from './components/Page'
export { Toolbar } from './components/Toolbar'
export { Button, IconButton } from './components/Button'
export { Badge, Chip, type Tone } from './components/Badge'
export { Card } from './components/Card'
export { ListSection, ListItem } from './components/List'
export { TextField, TextArea, SearchField } from './components/Fields'
export { Segmented, type SegmentOption } from './components/Segmented'
export { Toggle } from './components/Toggle'
export { Spinner, EmptyState } from './components/Spinner'
export { Modal } from './components/Modal'
export { ToastProvider, useToast } from './components/Toast'
