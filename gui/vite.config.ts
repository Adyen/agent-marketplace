import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import { guiUiOptions } from './ui-options'

export default defineConfig({
  plugins: [vue(), ui(guiUiOptions)],
  build: {
    outDir: '../cmd/agent-marketplace-gui/frontend/dist',
    emptyOutDir: true,
  },
})
