<script setup lang="ts">
import { useDocsVersion } from '../useDocsVersion'

const { current, href } = useDocsVersion()
</script>

<template>
  <aside v-if="current" class="docs-version-banner" :class="{ preview: current.version === 'preview' }" aria-label="Documentation release">
    <template v-if="current.version === 'preview'">
      Preview documentation follows development and may describe unreleased features.
      <a :href="href('latest')">Read the latest release.</a>
    </template>
    <template v-else-if="current.version === 'latest'">
      Documentation for <strong>{{ current.release }}</strong> (latest).
      <a :href="href(current.release!)">Permalink to this version.</a>
    </template>
    <template v-else>
      You are reading the <strong>{{ current.version }}</strong> documentation snapshot.
      <a :href="href('latest')">Read the latest release.</a>
    </template>
  </aside>
</template>

<style scoped>
.docs-version-banner {
  margin-bottom: 24px;
  padding: 12px 16px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  background: var(--vp-c-bg-soft);
  color: var(--vp-c-text-2);
  font-size: 14px;
  line-height: 1.6;
}

.preview {
  border-color: var(--vp-c-warning-1);
}

a {
  margin-left: 4px;
  color: var(--vp-c-brand-1);
  text-decoration: underline;
  text-underline-offset: 3px;
}

a:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 3px;
}
</style>
