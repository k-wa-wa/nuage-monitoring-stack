import type { StorybookConfig } from '@storybook/react-vite';

const config: StorybookConfig = {
  "stories": [
    "../src/**/*.mdx",
    "../src/**/*.stories.@(js|jsx|mjs|ts|tsx)"
  ],
  "addons": [
    "@storybook/addon-a11y",
    "@storybook/addon-docs"
  ],
  "framework": "@storybook/react-vite",
  // アプリ本体の vite.config.ts から VitePWA プラグインを継承すると、
  // Storybook のビルド出力(manager/preview bundle)まで Service Worker が
  // プリキャッシュ対象にしようとしてビルドに失敗するため除外する。
  async viteFinal(viteConfig) {
    const stripPwaPlugins = (plugins: unknown): unknown => {
      if (!Array.isArray(plugins)) return plugins
      return plugins
        .map((plugin) => (Array.isArray(plugin) ? stripPwaPlugins(plugin) : plugin))
        .filter((plugin) => {
          const name = Array.isArray(plugin) ? undefined : (plugin as { name?: string })?.name
          return !name?.startsWith('vite-plugin-pwa')
        })
    }
    return {
      ...viteConfig,
      plugins: stripPwaPlugins(viteConfig.plugins)
    }
  }
};
export default config;