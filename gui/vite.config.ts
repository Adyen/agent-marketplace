import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

export default defineConfig({
  plugins: [vue(), ui()],
  build: {
    outDir: '../cmd/agent-marketplace-gui/frontend/dist',
    emptyOutDir: true,
  },
})
