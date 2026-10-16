<script setup lang="ts">
import { appAssetPath, MARKETPLACE_DOCS_URL } from '~/lib/site'

const route = useRoute()
const config = useRuntimeConfig()
const marketplacePath = config.app.baseURL
const brandAsset = (name: string) => appAssetPath(marketplacePath, `brand/${name}`)
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
      <a :href="marketplacePath" class="am-site-nav__brand" aria-label="agent-marketplace" @click="closeMenu()">
        <img :src="brandAsset('logo-horizontal-black.svg')" alt="" class="am-site-nav__brand-logo am-site-nav__brand-logo--light">
        <img :src="brandAsset('logo-horizontal-white.svg')" alt="" class="am-site-nav__brand-logo am-site-nav__brand-logo--dark">
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
          <a :href="MARKETPLACE_DOCS_URL" target="_blank" rel="noopener noreferrer" class="am-site-nav__link" @click="closeMenu()">
            <span>Docs</span>
            <UIcon name="i-lucide-external-link" class="size-3.5 shrink-0" data-testid="docs-external-icon" aria-hidden="true" />
          </a>
        </div>

        <div class="am-site-nav__utilities">
          <MarketplaceThemeSwitch />
          <GitHubLink />
        </div>
      </div>
    </nav>
  </header>
</template>
