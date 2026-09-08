<script setup lang="ts">
import { withBase } from 'vitepress'
import {
  DOCUMENTATION_URL,
  GITHUB_REPOSITORY_URL,
  MARKETPLACE_PATH,
} from '../site'

const links = [
  { label: 'Documentation', href: DOCUMENTATION_URL, external: true },
  { label: 'Marketplace', href: MARKETPLACE_PATH, external: false, hardNavigation: true },
  { label: 'GitHub', href: GITHUB_REPOSITORY_URL, external: true },
]
</script>

<template>
  <footer class="utility-footer">
    <div class="am-wrap footer-row">
      <a class="brand" :href="withBase('/')">agent-marketplace</a>

      <nav aria-label="Footer navigation">
        <a
          v-for="link in links"
          :key="link.label"
          :href="link.href"
          :target="link.external ? '_blank' : link.hardNavigation ? '_self' : undefined"
          :rel="link.external ? 'noreferrer' : undefined"
        >
          {{ link.label }}
        </a>
      </nav>

      <p class="footer-meta">
        built at Adyen
        <span aria-hidden="true">·</span>
        Apache-2.0
        <span aria-hidden="true">·</span>
        No telemetry by default
      </p>
    </div>
  </footer>
</template>

<style scoped>
.utility-footer {
  padding: 22px 0;
  border-top: 1px solid var(--am-border);
  background: var(--am-footer-bg);
}

.footer-row {
  display: grid;
  grid-template-columns: auto auto minmax(0, 1fr);
  align-items: center;
  gap: 24px;
}

.brand {
  color: var(--vp-c-brand-1);
  font-family: var(--vp-font-family-mono);
  font-size: 17px;
  font-weight: 650;
  text-decoration: none;
  white-space: nowrap;
}

nav {
  display: flex;
  gap: 18px;
  white-space: nowrap;
}

nav a {
  color: var(--vp-c-text-2);
  font-size: 15px;
  font-weight: 550;
  text-decoration: none;
}

nav a:hover,
.brand:hover {
  color: var(--vp-c-brand-2);
}

nav a:focus-visible,
.brand:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 4px;
  border-radius: 2px;
}

.footer-meta {
  justify-self: end;
  margin: 0;
  color: var(--vp-c-text-3);
  font-family: var(--vp-font-family-mono);
  font-size: 12.5px;
  white-space: nowrap;
}

.footer-meta span {
  margin: 0 5px;
}

@media (max-width: 900px) {
  .footer-row {
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .footer-meta {
    justify-self: start;
  }

  nav {
    overflow-x: auto;
  }
}
</style>
