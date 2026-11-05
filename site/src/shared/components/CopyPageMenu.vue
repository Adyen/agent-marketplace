<script setup lang="ts">
import { useRoute, useData, withBase } from 'vitepress'
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'

import { useClipboard } from '../composables/useClipboard'

const route = useRoute()
const { site, theme } = useData()
const menuRoot = ref<HTMLElement>()
const menu = ref<HTMLElement>()
const isOpen = ref(false)
const { announcement, state, writeText } = useClipboard('Copied')
const isDocumentationPage = computed(
  () => theme.value.siteProfile === 'docs' || route.path.startsWith(withBase('/docs/')),
)

const markdownURL = computed(() => {
  const base = site.value.base
  const routePath = base !== '/' && route.path.startsWith(base)
    ? `/${route.path.slice(base.length)}`
    : route.path
  const cleanPath = routePath.replace(/\.html$/, '')
  const markdownPath = cleanPath.endsWith('/') ? `${cleanPath}index.md` : `${cleanPath}.md`
  return withBase(markdownPath)
})

function closeMenu(returnFocus = false) {
  isOpen.value = false
  if (returnFocus) menuRoot.value?.querySelector('button')?.focus()
}

async function toggleMenu() {
  isOpen.value = !isOpen.value
  if (isOpen.value) {
    await nextTick()
    menu.value?.querySelector<HTMLElement>('[role="menuitem"]')?.focus()
  }
}

function handleMenuKeydown(event: KeyboardEvent) {
  const items = [...(menu.value?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])]
  const current = items.indexOf(document.activeElement as HTMLElement)
  if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu(true)
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const direction = event.key === 'ArrowDown' ? 1 : -1
    items[(current + direction + items.length) % items.length]?.focus()
  }
}

function handleOutsideClick(event: PointerEvent) {
  if (menuRoot.value && !menuRoot.value.contains(event.target as Node)) closeMenu()
}

function copyPage() {
  return writeText(async () => {
    const response = await fetch(markdownURL.value)
    if (!response.ok) throw new Error('Markdown source unavailable')
    return response.text()
  })
}

onMounted(() => document.addEventListener('pointerdown', handleOutsideClick))
onUnmounted(() => document.removeEventListener('pointerdown', handleOutsideClick))
</script>

<template>
  <div v-if="isDocumentationPage" ref="menuRoot" class="copy-page-menu">
    <button
      class="copy-page-menu__trigger"
      type="button"
      aria-haspopup="menu"
      :aria-expanded="isOpen"
      data-copy-page-trigger
      @click="toggleMenu"
    >
      <svg aria-hidden="true" viewBox="0 0 16 16" fill="none">
        <rect x="5.5" y="2.5" width="8" height="10" rx="1.5" />
        <path d="M10.5 13.5v.25a1.75 1.75 0 0 1-1.75 1.75h-5A1.75 1.75 0 0 1 2 13.75v-8A1.75 1.75 0 0 1 3.75 4h.25" />
      </svg>
      <span>Copy page</span>
      <svg class="copy-page-menu__chevron" :class="{ 'is-open': isOpen }" aria-hidden="true" viewBox="0 0 16 16" fill="none">
        <path d="m4 6 4 4 4-4" />
      </svg>
    </button>
    <div
      ref="menu"
      v-show="isOpen"
      class="copy-page-menu__items"
      role="menu"
      aria-label="Page actions"
      @keydown="handleMenuKeydown"
    >
      <button class="copy-page-menu__item" type="button" role="menuitem" @click="copyPage">
        <svg aria-hidden="true" viewBox="0 0 16 16" fill="none">
          <rect x="5.5" y="2.5" width="8" height="10" rx="1.5" />
          <path d="M10.5 13.5v.25a1.75 1.75 0 0 1-1.75 1.75h-5A1.75 1.75 0 0 1 2 13.75v-8A1.75 1.75 0 0 1 3.75 4h.25" />
        </svg>
        <span>{{ state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : 'Copy page as Markdown' }}</span>
      </button>
      <a
        class="copy-page-menu__item"
        role="menuitem"
        :href="markdownURL"
        target="_blank"
        rel="noopener"
        data-copy-page-view
      >
        <svg aria-hidden="true" viewBox="0 0 16 16" fill="none">
          <path d="M4 1.75h5.5L13 5.25v8A1.75 1.75 0 0 1 11.25 15h-6.5A1.75 1.75 0 0 1 3 13.25v-9.75A1.75 1.75 0 0 1 4.75 1.75Z" />
          <path d="M9 2v3.5h3.5M5.5 8h5M5.5 10.5h5" />
        </svg>
        <span>View as Markdown</span>
      </a>
    </div>
    <span class="copy-page-menu__status" role="status" aria-live="polite">{{ announcement }}</span>
  </div>
</template>

<style scoped>
.copy-page-menu {
  position: relative;
  display: flex;
  justify-content: flex-end;
  margin-block-end: var(--lp-space-related);
}

.copy-page-menu__trigger {
  display: inline-flex;
  min-height: var(--lp-h-compact);
  align-items: center;
  gap: var(--lp-space-tight);
  padding: 0 var(--lp-space-related);
  border: 0;
  border-radius: var(--lp-radius-control);
  background: var(--lp-canvas);
  box-shadow: var(--lp-outline-shadow);
  color: var(--lp-text);
  font: var(--lp-font-nav);
  cursor: pointer;
}

.copy-page-menu__trigger:hover,
.copy-page-menu__item:hover {
  background: var(--lp-surface);
}

.copy-page-menu__trigger:focus-visible,
.copy-page-menu__item:focus-visible {
  outline: var(--lp-focus-width) solid var(--lp-focus);
  outline-offset: var(--lp-focus-offset);
}

.copy-page-menu__trigger svg,
.copy-page-menu__item svg {
  width: var(--lp-icon-size);
  height: var(--lp-icon-size);
  flex: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.5;
}

.copy-page-menu__chevron {
  width: 0.875rem !important;
  height: 0.875rem !important;
  transition: transform var(--lp-motion-feedback) var(--lp-ease);
}

.copy-page-menu__chevron.is-open {
  transform: rotate(180deg);
}

.copy-page-menu__items {
  position: absolute;
  z-index: 20;
  top: calc(100% + var(--lp-space-tight));
  right: 0;
  display: grid;
  width: max-content;
  max-width: calc(100vw - 2 * var(--lp-gutter));
  gap: 0.125rem;
  padding: var(--lp-space-tight);
  border: 0;
  border-radius: var(--lp-radius-control);
  background: var(--lp-canvas);
  box-shadow: var(--lp-outline-shadow), 0 0.5rem 1.5rem var(--lp-outline);
}

.copy-page-menu__item {
  display: flex;
  min-height: var(--lp-hit-target);
  align-items: center;
  gap: var(--lp-space-related);
  padding: var(--lp-space-tight) var(--lp-space-related);
  border: 0;
  border-radius: calc(var(--lp-radius-control) / 2);
  background: transparent;
  color: var(--lp-text);
  font: var(--lp-font-nav);
  text-align: left;
  text-decoration: none;
  white-space: nowrap;
  cursor: pointer;
}

.copy-page-menu__status {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  clip-path: inset(50%);
}
</style>
