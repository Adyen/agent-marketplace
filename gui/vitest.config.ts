import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

export default defineConfig({
  plugins: [vue(), ui()],
  test: {
    include: ['src/**/*.test.ts', 'tests/drift.spec.ts'],
  },
})
