import { defineConfig, mergeConfig } from 'vite'
import baseConfig from './vite.config.ts'
import { mockLocalApi } from './preview/mock-api.ts'

export default mergeConfig(
  baseConfig,
  defineConfig({
    plugins: [mockLocalApi()],
    server: { host: '127.0.0.1', port: 5199, strictPort: true },
  }),
)
