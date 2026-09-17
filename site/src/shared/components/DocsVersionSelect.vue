<script setup lang="ts">
import { useRouter } from 'vitepress'
import { useDocsVersion } from '../useDocsVersion'

defineProps<{ screenMenu?: boolean }>()
const router = useRouter()
const { versions, current, href } = useDocsVersion()

function selectVersion(event: Event) {
  void router.go(href((event.target as HTMLSelectElement).value))
}
</script>

<template>
  <label v-if="current" class="docs-version-select" :class="{ 'screen-menu': screenMenu }">
    <span>Version</span>
    <select aria-label="Documentation version" :value="current.version" @change="selectVersion">
      <option v-for="version in versions" :key="version.version" :value="version.version">
        {{ version.label }}
      </option>
    </select>
  </label>
</template>

<style scoped>
.docs-version-select {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  font-size: 13px;
  color: var(--vp-c-text-2);
}

select {
  max-width: 190px;
  padding: 5px 8px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  background: var(--vp-c-bg);
  color: var(--vp-c-text-1);
  font: inherit;
  cursor: pointer;
  appearance: auto;
}

select:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 3px;
}

.screen-menu {
  padding: 16px 0;
  justify-content: space-between;
}
</style>
