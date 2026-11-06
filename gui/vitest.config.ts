import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import { guiUiOptions } from './ui-options'

export default defineConfig({
  plugins: [vue(), ui(guiUiOptions)],
  test: {
    include: ['src/**/*.test.ts', 'tests/drift.spec.ts'],
  },
})
