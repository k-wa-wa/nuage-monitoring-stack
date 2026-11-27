import { defineConfig, devices } from '@playwright/test'

const e2ePort = Number(process.env.E2E_PORT || 4175)
const baseURL = `http://127.0.0.1:${e2ePort}`

const chromiumExec = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
const { defaultBrowserType: _iphoneBrowserType, ...iphone14Descriptor } = devices['iPhone 14']

export default defineConfig({
	testDir: './e2e',
	fullyParallel: false,
	workers: 1,
	retries: process.env.CI ? 1 : 0,
	reporter: [['list']],
	use: {
		baseURL,
		serviceWorkers: 'block',
		screenshot: 'only-on-failure'
	},
	projects: [
		{
			name: 'android-chromium',
			use: {
				...devices['Pixel 7'],
				...(chromiumExec
					? { launchOptions: { executablePath: chromiumExec } }
					: {})
			}
		},
		{
			name: 'iphone-chromium',
			use: {
				...iphone14Descriptor,
				browserName: 'chromium',
				...(chromiumExec
					? { launchOptions: { executablePath: chromiumExec } }
					: {})
			}
		}
	],
	webServer: {
		command: 'node e2e/test-server.mjs',
		port: e2ePort,
		reuseExistingServer: !process.env.CI,
		env: {
			E2E_PORT: String(e2ePort)
		}
	}
})
