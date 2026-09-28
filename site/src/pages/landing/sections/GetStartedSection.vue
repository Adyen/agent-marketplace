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
    density="compact"
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

    <ActionLink class="quick-start" :href="quickStartPath">
      Quick start
      <span aria-hidden="true">→</span>
    </ActionLink>
  </SectionShell>
</template>

<style scoped>
.closing {
  border-color: var(--am-accent-border);
}

.product-name {
  color: var(--vp-c-brand-1);
}

.bar {
  --am-technical-font-size: var(--am-text-body-sm);
  --am-technical-padding: 18px;
  width: 100%;
  max-width: 960px;
  padding-block: 15px;
  box-shadow: 0 12px 26px -20px var(--am-code-shadow);
}

.quick-start {
  margin-top: 20px;
}

@media (max-width: 680px) {
  .quick-start {
    width: 100%;
  }
}
</style>
