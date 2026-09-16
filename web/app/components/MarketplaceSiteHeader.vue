<script setup lang="ts">
import { DOCS_PATH } from '~/lib/site'

const route = useRoute()
const menuOpen = ref(false)
const menuButton = ref<HTMLButtonElement | null>(null)

watch(() => route.fullPath, () => {
  menuOpen.value = false
})

function closeMenu(returnFocus = false) {
  menuOpen.value = false
  if (returnFocus) menuButton.value?.focus()
}
</script>

<template>
  <header class="am-site-header">
    <nav class="am-site-nav" aria-label="Product navigation" @keydown.esc="closeMenu(true)">
      <a href="/" class="am-site-nav__brand" @click="closeMenu()">
        agent-marketplace
      </a>

      <button
        ref="menuButton"
        type="button"
        class="am-site-nav__menu-button"
        aria-label="Toggle navigation"
        aria-controls="site-navigation-actions"
        :aria-expanded="menuOpen"
        @click="menuOpen = !menuOpen"
      >
        <span class="am-site-nav__menu-icon" aria-hidden="true" />
      </button>

      <div id="site-navigation-actions" class="am-site-nav__actions" :data-open="menuOpen">
        <div class="am-site-nav__links">
          <NuxtLink to="/" class="am-site-nav__link" @click="closeMenu()">
            Marketplace
          </NuxtLink>
          <a :href="DOCS_PATH" class="am-site-nav__link" @click="closeMenu()">Docs</a>
        </div>

        <div class="am-site-nav__utilities">
          <MarketplaceThemeSwitch />
          <GitHubLink />
        </div>
      </div>
    </nav>
  </header>
</template>
