<script setup lang="ts">
defineProps<{
  label: string
  href: string
  screenMenu?: boolean
}>()
</script>

<template>
  <a
    class="SiteNavLink"
    :class="{ 'screen-menu': screenMenu }"
    :href="href"
    target="_self"
  >{{ label }}</a>
</template>

<style scoped>
.SiteNavLink {
  display: flex;
  align-items: center;
  height: var(--lp-h-compact);
  padding: 0 var(--lp-space-related);
  color: var(--lp-text-2);
  font: var(--lp-font-nav);
  transition: color var(--lp-motion-feedback) var(--lp-ease);
}

.SiteNavLink:hover {
  color: var(--lp-text);
}

.SiteNavLink.screen-menu {
  display: block;
  height: auto;
  border-bottom: 1px solid var(--lp-divider);
  padding: var(--lp-space-related) 0;
  line-height: 1.34;
  transition:
    border-color var(--lp-motion-feedback) var(--lp-ease),
    color var(--lp-motion-feedback) var(--lp-ease);
}
</style>
