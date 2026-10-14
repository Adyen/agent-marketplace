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
  padding: 0 12px;
  line-height: var(--vp-nav-height);
  font-size: 14px;
  font-weight: 500;
  color: var(--vp-c-text-1);
  transition: color 0.25s;
}

.SiteNavLink:hover {
  color: var(--vp-c-brand-1);
}

.SiteNavLink.screen-menu {
  display: block;
  border-bottom: 1px solid var(--vp-c-divider);
  padding: 12px 0 11px;
  line-height: 24px;
  transition:
    border-color 0.25s,
    color 0.25s;
}
</style>
