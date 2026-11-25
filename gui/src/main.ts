import './style.css'

import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import ui from '@nuxt/ui/vue-plugin'
import App from './App.vue'
import WorkspaceView from './WorkspaceView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [{ path: '/', component: WorkspaceView }],
})

createApp(App).use(router).use(ui).mount('#app')
