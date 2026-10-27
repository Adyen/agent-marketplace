<script setup lang="ts">
import { withBase } from 'vitepress'
import { DOCS_PATH, INSTALL_COMMAND } from '../../../shared/site'
import ActionLink from '../components/ActionLink.vue'
import CommandBar from '../components/CommandBar.vue'
import SectionShell from '../components/SectionShell.vue'

const quickStartPath = withBase(`${DOCS_PATH}quick-start`)
</script>

<template>
  <SectionShell
    id="install"
    class="closing"
    eyebrow="Get started"
    title="Ready to try agent-marketplace?"
  >
    <template #title>
      Ready to try <span class="product-name">agent-marketplace</span>?
    </template>
    <template #sub>
      Install one CLI, then sync shared agent tooling into each harness’s native layout.
    </template>

    <CommandBar :command="INSTALL_COMMAND" class="bar" />

    <ActionLink
      class="quick-start"
      :href="quickStartPath"
      label="Quick start"
      icon="arrow"
    />
  </SectionShell>
</template>

<style scoped>
.closing :deep(h2) {
  font: var(--lp-font-cta);
  text-wrap: balance;
}

.product-name {
  color: var(--lp-text-muted);
  white-space: nowrap;
}

.bar {
  max-width: calc(50% - var(--lp-space-related));
}

.quick-start {
  display: flex;
  margin-top: var(--lp-space-default);
}

@media (max-width: 639px) {
  .product-name {
    white-space: normal;
  }
}

@media (max-width: 959px) {
  .bar {
    max-width: none;
  }
}
</style>
