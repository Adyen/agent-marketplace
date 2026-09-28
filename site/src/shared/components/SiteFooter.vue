<script setup lang="ts">
import { withBase } from 'vitepress'
import { DOCS_PATH, GITHUB_REPOSITORY_URL } from '../site'

const links = [
  { label: 'Apache 2.0 license', href: `${GITHUB_REPOSITORY_URL}/blob/main/LICENSE`, external: true },
  { label: 'Documentation', href: withBase(DOCS_PATH) },
  { label: 'GitHub ↗', href: GITHUB_REPOSITORY_URL, external: true },
]
</script>

<template>
  <footer class="site-footer">
    <div class="footer-row">
      <p>
        Built at <span class="adyen">Adyen</span>
      </p>

      <nav aria-label="Footer navigation">
        <template v-for="(link, index) in links" :key="link.label">
          <span v-if="index" aria-hidden="true">·</span>
          <a
            :href="link.href"
            :target="link.external ? '_blank' : undefined"
            :rel="link.external ? 'noreferrer' : undefined"
          >
            {{ link.label }}
          </a>
        </template>
      </nav>
    </div>
  </footer>
</template>

<style scoped>
.site-footer {
  padding: 24px 0;
  border-top: 1px solid var(--am-border);
  background: var(--vp-c-bg);
  font-family: var(--vp-font-family-base), sans-serif;
}

.footer-row {
  display: flex;
  max-width: var(--am-maxw);
  align-items: center;
  justify-content: space-between;
  gap: 16px 32px;
  margin: 0 auto;
  padding: 0 24px;
}

p,
nav {
  margin: 0;
  color: var(--vp-c-text-2);
  font-size: var(--am-text-body-sm);
  font-weight: var(--am-font-medium);
}

.adyen {
  color: var(--vp-c-brand-1);
  font-weight: var(--am-font-semibold);
}

nav {
  display: flex;
  align-items: center;
  gap: 10px;
}

a {
  color: inherit;
  text-decoration: none;
  white-space: nowrap;
}

a:hover {
  color: var(--vp-c-brand-2);
}

a:focus-visible {
  border-radius: 2px;
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 3px;
}

@media (max-width: 680px) {
  .footer-row {
    align-items: flex-start;
    flex-direction: column;
  }

  nav {
    flex-wrap: wrap;
  }
}
</style>
