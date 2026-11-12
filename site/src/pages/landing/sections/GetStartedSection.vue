<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { withBase } from 'vitepress'
import {
  detectGuiVisitorOS,
  DOCS_PATH,
  fallbackGuiDownloads,
  INSTALL_COMMAND,
  loadGuiDownloads,
  type GuiDownloadSelection,
} from '../../../shared/site'
import ActionLink from '../components/ActionLink.vue'
import CommandBar from '../components/CommandBar.vue'
import SectionShell from '../components/SectionShell.vue'

const quickStartPath = withBase(`${DOCS_PATH}quick-start`)
const guiDownloads = ref<GuiDownloadSelection>(fallbackGuiDownloads())

onMounted(async () => {
  guiDownloads.value = await loadGuiDownloads(
    detectGuiVisitorOS(navigator.platform, navigator.userAgent),
  )
})
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
      Install the desktop app or use one CLI to sync shared agent tooling into each harness's native layout.
    </template>

    <div class="install-options">
      <div class="install-option">
        <h3>Command line</h3>
        <CommandBar :command="INSTALL_COMMAND" class="bar" />
        <ActionLink
          class="quick-start"
          :href="quickStartPath"
          label="Quick start"
          icon="arrow"
        />
      </div>

      <div class="install-option">
        <h3>Desktop app</h3>
        <div class="gui-downloads">
          <a
            v-if="guiDownloads.status === 'fallback'"
            class="gui-download gui-download-primary"
            :href="guiDownloads.url"
          >
            {{ guiDownloads.label }}
          </a>
          <a
            v-if="guiDownloads.status === 'available' && guiDownloads.primary"
            class="gui-download gui-download-primary"
            :href="guiDownloads.primary.url"
          >
            {{ guiDownloads.primary.label }}
          </a>
          <a
            v-for="download in guiDownloads.status === 'available' ? guiDownloads.links : []"
            :key="download.url"
            class="gui-download"
            :href="download.url"
          >
            {{ download.label }}
          </a>
        </div>
        <p class="first-launch">
          First launch on macOS: Right-click the app and choose Open. If it is blocked again, open
          System Settings → Privacy &amp; Security and choose "Open Anyway".
        </p>
      </div>
    </div>
  </SectionShell>
</template>

<style scoped>
.install-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--lp-space-default);
}

.install-option {
  min-width: 0;
}

.install-option h3 {
  margin: 0 0 var(--lp-space-related);
  color: var(--lp-text-muted);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.closing :deep(h2) {
  font: var(--lp-font-cta);
  text-wrap: balance;
}

.product-name {
  color: var(--lp-text-muted);
  white-space: nowrap;
}

.bar {
  max-width: none;
}

.quick-start {
  display: flex;
  margin-top: var(--lp-space-default);
}

.gui-downloads {
  display: flex;
  flex-wrap: wrap;
  gap: var(--lp-space-tight);
}

.gui-download {
  display: inline-flex;
  min-height: var(--lp-h-control);
  align-items: center;
  padding: 0 var(--lp-inset);
  border-radius: var(--lp-radius-control);
  background: var(--lp-fill);
  color: var(--lp-text);
  font: var(--lp-font-action);
  text-decoration: none;
}

.gui-download-primary {
  background: var(--lp-green);
  color: var(--lp-on-green);
}

.first-launch {
  max-width: 38rem;
  margin: var(--lp-space-related) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-body);
}

@media (max-width: 639px) {
  .install-options {
    grid-template-columns: 1fr;
  }

  .product-name {
    white-space: normal;
  }
}
</style>
