import type { Preview } from '@storybook/react-vite'
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@fontsource/ibm-plex-sans-jp/400.css'
import '@fontsource/ibm-plex-sans-jp/600.css'
import '@fontsource/ibm-plex-mono/400.css'
import '../src/styles/index.css'

const preview: Preview = {
	parameters: {
		layout: 'fullscreen',
		backgrounds: { disable: true }
	},
	decorators: [
		(Story) => (
			<div style={{ position: 'relative', minHeight: '100vh', padding: 16, background: 'var(--bg)', color: 'var(--fg)' }}>
				<Story />
			</div>
		)
	]
}

export default preview
