<script setup lang="ts">
import { useData, withBase } from 'vitepress'
import { GITHUB_REPOSITORY_URL } from '../site'

const { theme } = useData()
const documentationPath = theme.value.siteProfile === 'docs' ? withBase('/') : withBase('/docs/')

const links = [
  { label: 'Apache 2.0 license', href: `${GITHUB_REPOSITORY_URL}/blob/main/LICENSE`, external: true },
  { label: 'Documentation', href: documentationPath },
  { label: 'GitHub ↗', href: GITHUB_REPOSITORY_URL, external: true },
]
</script>

<template>
  <footer class="site-footer am-rule-top">
    <div class="footer-row">
      <p>
        Built at <span class="adyen">Adyen</span>
      </p>

      <nav aria-label="Footer navigation">
        <template v-for="(link, index) in links" :key="link.label">
          <span v-if="index" aria-hidden="true">·</span>
          <a
            class="am-hit-target"
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
  padding: var(--lp-space-default) 0;
  background: var(--lp-canvas);
  font-family: var(--lp-sans);
}

.footer-row {
  display: flex;
  width: min(100% - 2 * var(--lp-gutter), var(--lp-content-max));
  max-width: none;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--lp-space-related) var(--lp-space-default);
  margin-inline: auto;
  padding: 0;
}

p,
nav {
  margin: 0;
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.adyen {
  color: var(--lp-text);
  font-weight: inherit;
}

nav {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--lp-space-related);
}

nav span {
  color: var(--lp-text-muted);
}

a {
  display: inline-flex;
  min-height: 1.5rem;
  align-items: center;
  color: inherit;
  text-decoration: none;
  white-space: nowrap;
  transition: color var(--lp-motion-feedback) var(--lp-ease);
}

a:hover {
  color: var(--lp-text);
}

@media (max-width: 959px) {
  nav {
    row-gap: var(--lp-space-default);
  }

}

@media (prefers-reduced-motion: reduce) {
  a {
    transition: none;
  }
}

@media (max-width: 639px) {
  .footer-row {
    align-items: flex-start;
    flex-direction: column;
  }

  nav {
    flex-wrap: wrap;
  }
}
</style>
