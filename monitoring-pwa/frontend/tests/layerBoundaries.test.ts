import { describe, expect, test } from 'bun:test'
import fs from 'node:fs'
import path from 'node:path'

const SRC_ROOT = path.resolve(import.meta.dir, '../src')

type Layer =
	| 'types'
	| 'utils'
	| 'api'
	| 'state'
	| 'routes'
	| 'hooks'
	| 'components'
	| 'pages'
	| 'entry'

const ALLOWED_INTERNAL: Record<Layer, Set<Layer>> = {
	types: new Set(['types']),
	utils: new Set(['utils', 'types']),
	api: new Set(['api', 'utils', 'types']),
	state: new Set(['state', 'api', 'utils', 'types']),
	routes: new Set(['routes', 'types']),
	hooks: new Set(['hooks', 'api', 'state', 'utils', 'types']),
	// 仕様では純粋 UI 用 hooks のみ許可だが、oxlint の先読み制約と現状未使用を踏まえ、安全側で一律禁止としている
	components: new Set(['components', 'utils', 'types']),
	pages: new Set(['pages', 'components', 'state', 'hooks', 'routes', 'utils', 'types']),
	entry: new Set(['entry', 'components', 'state', 'hooks', 'routes', 'utils', 'types', 'pages'])
}

const DISALLOWED_PACKAGES: Partial<Record<Layer, RegExp>> = {
	types: /^(?:react|react-dom|react-router-dom|@nuage\/ui)(?:\/|$)/,
	utils: /^(?:react|react-dom|react-router-dom|@nuage\/ui)(?:\/|$)/,
	api: /^(?:react|react-dom|react-router-dom|@nuage\/ui)(?:\/|$)/
}

function collectFiles(dir: string): string[] {
	const out: string[] = []
	for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
		const full = path.join(dir, entry.name)
		if (entry.isDirectory()) {
			out.push(...collectFiles(full))
		} else if (/\.(ts|tsx)$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
			out.push(full)
		}
	}
	return out
}

function layerOf(filePath: string): Layer {
	const rel = path.relative(SRC_ROOT, filePath).replace(/\\/g, '/')
	const seg = rel.split('/')[0]
	if (seg === 'App.tsx' || seg === 'main.tsx' || seg === 'sw.ts') return 'entry'
	if (
		seg === 'types' ||
		seg === 'utils' ||
		seg === 'api' ||
		seg === 'state' ||
		seg === 'routes' ||
		seg === 'hooks' ||
		seg === 'components' ||
		seg === 'pages'
	) {
		return seg
	}
	throw new Error(`未知のディレクトリ層: ${rel}`)
}

function extractSpecifiers(code: string): string[] {
	const specs: string[] = []
	const re = /(?:import|export)\s+(?:type\s+)?(?:[^'"]*?\s+from\s+)?['"]([^'"]+)['"]/g
	let m: RegExpExecArray | null
	while ((m = re.exec(code)) !== null) {
		specs.push(m[1])
	}
	return specs
}

describe('フロントエンド層境界', () => {
	test('すべてのファイルが許可された層・パッケージのみに依存している', () => {
		const files = collectFiles(SRC_ROOT)
		const violations: string[] = []

		for (const file of files) {
			const fromLayer = layerOf(file)
			const relFrom = path.relative(SRC_ROOT, file).replace(/\\/g, '/')
			const code = fs.readFileSync(file, 'utf8')
			for (const spec of extractSpecifiers(code)) {
				if (spec.startsWith('.')) {
					if (/\.(css|svg|png|json)$/.test(spec)) continue
					const resolved = path.resolve(path.dirname(file), spec)
					if (!resolved.startsWith(SRC_ROOT)) {
						violations.push(`${relFrom}: src 外の相対参照 (${spec})`)
						continue
					}
					const targetLayer = layerOf(resolved)
					if (!ALLOWED_INTERNAL[fromLayer].has(targetLayer)) {
						violations.push(`${relFrom} (${fromLayer}) -> ${spec} (${targetLayer}) は禁止`)
					}
				} else {
					const pkgPattern = DISALLOWED_PACKAGES[fromLayer]
					if (pkgPattern && pkgPattern.test(spec)) {
						violations.push(`${relFrom} (${fromLayer}) -> ${spec} は禁止`)
					}
				}
			}
		}

		expect(violations).toEqual([])
	})
})
