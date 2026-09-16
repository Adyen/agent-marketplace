<script setup lang="ts">
import { MARKETPLACE_PATH } from '../site'

// Rendered as a `component` nav item: VPNavBarMenu (desktop) and
// VPNavScreenMenu (mobile, which passes `screen-menu`). A plain anchor keeps
// VitePress's nav-link normalization (normalizeLink/withBase) away from
// MARKETPLACE_PATH, so the sibling app stays at the origin root from every
// page depth and under any SITE_BASE. target="_self" keeps the router from
// intercepting the click as an SPA route.
defineProps<{ screenMenu?: boolean }>()
</script>

<template>
  <a
    class="MarketplaceNavLink"
    :class="{ 'screen-menu': screenMenu }"
    :href="MARKETPLACE_PATH"
    target="_self"
  >Marketplace</a>
</template>

<style scoped>
/* Mirrors the default theme's VPNavBarMenuLink / VPNavScreenMenuLink styles;
   the scoped class names there cannot be reused from outside the theme. */
.MarketplaceNavLink {
  display: flex;
  align-items: center;
  padding: 0 12px;
  line-height: var(--vp-nav-height);
  font-size: 14px;
  font-weight: 500;
  color: var(--vp-c-text-1);
  transition: color 0.25s;
}

.MarketplaceNavLink:hover {
  color: var(--vp-c-brand-1);
}

.MarketplaceNavLink.screen-menu {
  display: block;
  border-bottom: 1px solid var(--vp-c-divider);
  padding: 12px 0 11px;
  line-height: 24px;
  transition:
    border-color 0.25s,
    color 0.25s;
}
</style>
