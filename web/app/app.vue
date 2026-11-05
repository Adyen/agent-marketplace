<script setup lang="ts">
import { appAssetPath } from '~/lib/site'

const appBaseURL = useRuntimeConfig().app.baseURL
const brandAsset = (name: string) => appAssetPath(appBaseURL, `brand/${name}`)

useHead({
  link: [
    { rel: 'icon', type: 'image/svg+xml', href: brandAsset('favicon.svg') },
    { rel: 'icon', type: 'image/png', sizes: '32x32', href: brandAsset('favicon-32x32.png') },
    { rel: 'icon', type: 'image/png', sizes: '64x64', href: brandAsset('favicon-64x64.png') },
  ],
})
</script>

<template>
  <UApp>
    <MarketplaceSkipLink target="main-content" />
    <MarketplaceSiteHeader />
    <NuxtPage />
  </UApp>
</template>
