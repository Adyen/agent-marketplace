<script setup lang="ts">
import { INSTALL_COMMAND } from '../../../shared/site'
import CommandBar from '../components/CommandBar.vue'
import SectionShell from '../components/SectionShell.vue'
</script>

<template>
  <SectionShell
    id="install"
    class="closing"
    eyebrow="Get started"
    title="Ready to try agent-marketplace?"
    soft
  >
    <template #title>
      Ready to try <span class="product-name">agent-marketplace</span>?
    </template>
    <template #sub>
      Install one CLI, then sync shared agent tooling into each harness’s native layout.
    </template>

    <CommandBar :command="INSTALL_COMMAND" class="bar" />
  </SectionShell>
</template>

<style scoped>
.closing {
  --am-section-heading-gap: 24px;
  padding-block: 72px;
  border-color: var(--am-accent-border);
}

.product-name {
  color: var(--vp-c-brand-1);
}

.bar {
  --am-technical-font-size: 14px;
  --am-technical-padding: 18px;
  width: 100%;
  max-width: 960px;
  padding-block: 15px;
  box-shadow: 0 12px 26px -20px var(--am-code-shadow);
}

.bar :deep(.copy) {
  padding: 8px 12px;
  font-size: 12px;
}

@media (max-width: 680px) {
  .closing {
    padding-block: 64px;
  }
}
</style>
