<script setup lang="ts">
import { withBase } from 'vitepress'

const props = withDefaults(defineProps<{
  view: 'catalog' | 'detail'
  loading?: 'eager' | 'lazy'
}>(), {
  loading: 'lazy',
})

const appearances = ['light', 'dark'] as const
const altText = {
  catalog: 'Marketplace catalog showing skills, commands, and MCP servers with Kind and Registry filters and the Copy install command action.',
  detail: 'Marketplace resource detail showing a pinned revision digest and a clean Security result.',
}

function imagePath(width: 'desktop' | 'narrow', appearance: typeof appearances[number]) {
  return withBase(`/landing/${props.view}-${width}-${appearance}.webp`)
}
</script>

<template>
  <figure
    class="marketplace-browser-frame"
    role="group"
    :aria-label="props.view === 'catalog' ? 'Marketplace catalog preview' : 'Marketplace resource detail preview'"
    :data-web-ui-view="props.view"
  >
    <div class="marketplace-browser-frame__header" aria-hidden="true">
      <span class="marketplace-browser-frame__controls"><i /><i /><i /></span>
      <span class="marketplace-browser-frame__address">marketplace.example.com</span>
    </div>
    <div class="marketplace-browser-frame__viewport">
      <img
        v-for="appearance in appearances"
        :key="appearance"
        data-web-ui-image
        class="marketplace-browser-frame__image"
        :class="`marketplace-browser-frame__image--${appearance}`"
        :data-theme="appearance"
        :src="imagePath('desktop', appearance)"
        :srcset="`${imagePath('narrow', appearance)} 390w, ${imagePath('desktop', appearance)} 1440w`"
        sizes="100vw"
        width="1440"
        height="960"
        :alt="altText[props.view]"
        :loading="loading"
      >
    </div>
  </figure>
</template>

<style scoped>
.marketplace-browser-frame {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--lp-outline);
  border-radius: var(--lp-radius-preview);
  box-shadow: none;
}

.marketplace-browser-frame__header {
  display: flex;
  min-height: 2.75rem;
  align-items: center;
  gap: var(--lp-space-related);
  padding: 0 var(--lp-space-related);
  border-bottom: 1px solid var(--lp-outline);
  background: var(--lp-surface);
  color: var(--lp-text-2);
  font: var(--lp-font-label);
}

.marketplace-browser-frame__controls {
  display: flex;
  flex: 0 0 auto;
  gap: 0.375rem;
}

.marketplace-browser-frame__controls i {
  width: 0.55rem;
  height: 0.55rem;
  border: 1px solid var(--lp-outline);
  border-radius: 50%;
}

.marketplace-browser-frame__address {
  width: min(100%, 32rem);
  margin-inline: auto;
  padding: 0.35rem var(--lp-space-related);
  border: 1px solid var(--lp-outline);
  border-radius: var(--lp-radius-control);
  text-align: center;
}

.marketplace-browser-frame__image {
  display: block;
  width: 100%;
  height: auto;
}

.marketplace-browser-frame__image--dark {
  display: none;
}

:global(html.dark .marketplace-browser-frame__image--light) {
  display: none;
}

:global(html.dark .marketplace-browser-frame__image--dark) {
  display: block;
}
</style>
