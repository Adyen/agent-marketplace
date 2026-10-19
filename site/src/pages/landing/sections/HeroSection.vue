<script setup lang="ts">
import {
  GITHUB_REPOSITORY_URL,
  INSTALL_COMMAND,
} from '../../../shared/site'
import ActionLink from '../components/ActionLink.vue'
import CommandBar from '../components/CommandBar.vue'
import TuiPreview from '../components/TuiPreview.vue'
</script>

<template>
  <header class="hero">
    <div class="am-wrap">
      <div class="grid">
        <div class="hero-copy">
          <h1>
            Manage AI agent tooling once<span class="h1-sub">Sync it everywhere you work</span>
          </h1>
          <p class="lede">
              Manage skills, commands, hooks, and MCP servers from one config, then
              sync them to Claude, Cursor, Codex, and more.
          </p>

          <CommandBar :command="INSTALL_COMMAND" class="install" />

          <div class="actions">
            <ActionLink
              class="hero-action"
              :href="GITHUB_REPOSITORY_URL"
              variant="outline"
              target="_blank"
              rel="noreferrer"
            >
              View on GitHub
              <span aria-hidden="true">↗</span>
            </ActionLink>
          </div>
        </div>

        <div class="product-stage">
        <TuiPreview />
          <p class="preview-caption">
            Select resources, review their targets, then sync.
          </p>
        </div>
      </div>

    </div>
  </header>
</template>

<style scoped>
.hero {
  --hero-sans: "Adyen", "Inter", system-ui, sans-serif;
  --hero-mono: "Adyen Mono", "Geist Mono", ui-monospace, SFMono-Regular, monospace;
  --hero-ink: #111111;
  --hero-secondary: rgb(17 17 17 / 60.7%);
  --hero-tertiary: rgb(17 17 17 / 48%);
  --hero-line: rgb(17 17 17 / 17.8%);
  --hero-layer: rgb(17 17 17 / 7.4%);
  --hero-visual: #f5f5f5;
  --hero-visual-raised: #ededed;

  position: relative;
  z-index: 1;
  overflow: hidden;
  padding: 96px 0;
  background:
    radial-gradient(circle at 76% 44%, var(--am-hero-glow), transparent 31rem),
    linear-gradient(var(--am-hero-grid) 1px, transparent 1px),
    linear-gradient(90deg, var(--am-hero-grid) 1px, transparent 1px),
    var(--am-hero-bg);
  background-size: auto, 44px 44px, 44px 44px, auto;
  color: var(--am-hero-text);
  font-family: var(--hero-sans);
  font-feature-settings: "ss01", "ss02";
  font-optical-sizing: auto;
}

:global(.dark .hero) {
  --hero-ink: #ffffff;
  --hero-secondary: rgb(255 255 255 / 60.7%);
  --hero-tertiary: rgb(255 255 255 / 48%);
  --hero-line: rgb(255 255 255 / 17.8%);
  --hero-layer: rgb(255 255 255 / 7.4%);
  --hero-visual: #1b1b1b;
  --hero-visual-raised: #232323;
}

.hero::after {
  position: absolute;
  right: -14rem;
  bottom: -22rem;
  width: 44rem;
  height: 44rem;
  border: 1px solid var(--am-hero-ring);
  border-radius: 50%;
  background: radial-gradient(circle, var(--am-hero-glow), transparent 65%);
  content: "";
  pointer-events: none;
}

.grid {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: minmax(0, 7fr) minmax(420px, 5fr);
  gap: 24px;
  align-items: center;
}

.hero .am-wrap {
  max-width: 1440px;
  padding-inline: 48px;
}

.hero-copy {
  min-width: 0;
  padding-inline-end: 24px;
}

h1 {
  max-width: 760px;
  margin: 0 0 12px;
  color: var(--hero-ink);
  font-family: var(--hero-sans);
  font-size: 64px;
  font-weight: 452;
  line-height: 1.02;
  letter-spacing: normal;
  text-wrap: balance;
}

.h1-sub {
  color: var(--hero-tertiary);
  font: inherit;
  text-transform: lowercase;
}

.h1-sub::before {
  content: ", ";
}

.lede {
  max-width: 576px;
  margin: 0 0 24px;
  color: var(--hero-secondary);
  font-family: var(--hero-sans);
  font-size: 16px;
  font-weight: 400;
  line-height: 1.34;
  text-wrap: pretty;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 24px;
}

.install {
  --am-technical-font-size: 12px;
  --am-technical-line-height: 1.3;
  --am-technical-padding: 12px;
  min-height: 48px;
  box-shadow: none;
}

.product-stage {
  position: relative;
  min-width: 0;
}

.preview-caption {
  margin: 12px 0 0;
  color: var(--hero-secondary);
  font-size: 12px;
  line-height: 1.38;
}

:deep(.command-bar.install) {
  gap: 12px;
  height: 48px;
  padding: 0 6px 0 12px;
  border-color: var(--hero-line);
  border-radius: 6px;
  background: transparent;
  color: var(--hero-ink);
  font-family: var(--hero-mono);
}

.install :deep(code) {
  color: var(--hero-ink);
  font-family: var(--hero-mono);
  font-size: 12px;
  font-weight: 500;
  line-height: 1.3;
}

.install :deep(.prompt) {
  color: var(--hero-tertiary);
}

.install :deep(.highlight) {
  color: var(--hero-ink);
}

.install :deep(.copy) {
  min-height: 36px;
  padding-inline: 12px;
  border: 0;
  border-radius: 6px;
  background: var(--hero-layer);
  color: var(--hero-ink);
  font-family: var(--hero-mono);
  font-size: 12px;
  font-weight: 500;
  line-height: 1.3;
  text-transform: uppercase;
}

.install :deep(.copy:hover),
.install :deep(.copy.is-done) {
  border: 0;
  background: var(--hero-line);
  color: var(--hero-ink);
}

.hero :deep(.hero-action) {
  min-height: 48px;
  padding-inline: 24px;
  border: 0;
  border-radius: 6px;
  background: var(--hero-layer);
  color: var(--hero-ink);
  font-family: var(--hero-sans);
  font-size: 16px;
  font-weight: 500;
  line-height: 1.34;
  transform: none;
}

.hero :deep(.hero-action:hover) {
  border: 0;
  background: var(--hero-line);
  color: var(--hero-ink);
  transform: none;
}

.product-stage :deep(.terminal-frame) {
  border-color: var(--hero-line);
  border-radius: 12px;
  background: var(--hero-visual);
  box-shadow: none;
  color: var(--hero-ink);
  font-family: var(--hero-mono);
  font-size: 12px;
  line-height: 1.3;
  font-feature-settings: normal;
}

.product-stage :deep(.terminal-header) {
  min-height: 48px;
  /* (48px header - 9px dot) / 2 rounds to a shared 20px inset. */
  padding: 0 20px;
  border-color: var(--hero-line);
  background: var(--hero-visual-raised);
  color: var(--hero-tertiary);
  font-family: var(--hero-mono);
  font-size: 12px;
  font-weight: 500;
  line-height: 1.3;
  text-transform: uppercase;
}

.product-stage :deep(.window-controls) {
  gap: 6px;
}

.product-stage :deep(.window-controls i) {
  width: 9px;
  height: 9px;
}

.product-stage :deep(.terminal-status),
.product-stage :deep(.preview-status) {
  color: var(--hero-secondary);
}

.product-stage :deep(.screen) {
  padding: 18px;
}

.product-stage :deep(.title) {
  margin-bottom: 12px;
}

.product-stage :deep(.panes) {
  grid-template-columns: 1fr;
  gap: 12px;
  min-height: 0;
}

.product-stage :deep(.resources) {
  display: grid;
  gap: 12px;
  align-content: start;
}

.product-stage :deep(.registry + .registry) {
  margin-top: 0;
}

.product-stage :deep(.registry-title) {
  margin-bottom: 6px;
  font-weight: 500;
}

.product-stage :deep(.resource) {
  grid-template-columns: 24px 48px minmax(0, 1fr) 12px;
  min-height: 30px;
  padding-inline: 6px;
}

.product-stage :deep(.resource.selected),
.product-stage :deep(.details) {
  background: var(--hero-visual-raised);
}

.product-stage :deep(.details) {
  padding: 12px;
  border: 0;
  border-radius: 6px;
}

.product-stage :deep(.details dl) {
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 6px 12px;
}

.product-stage :deep(.activity) {
  gap: 12px;
  margin-top: 12px;
  padding-top: 12px;
  border-color: var(--hero-line);
  color: var(--hero-tertiary);
  font-size: 12px;
  line-height: 1.3;
}

.product-stage :deep(kbd) {
  padding: 3px 6px;
  border: 0;
  border-radius: 3px;
  background: var(--hero-visual-raised);
}

@media (max-width: 1024px) {
  .hero {
    padding-block: 72px;
  }

  .grid {
    grid-template-columns: 1fr;
    gap: 48px;
  }

  .hero-copy {
    max-width: 720px;
    padding-inline-end: 0;
  }

}

@media (max-width: 768px) {
  .hero .am-wrap {
    padding-inline: 24px;
  }
}

@media (max-width: 600px) {
  .hero {
    padding-block: 48px;
    background-size: auto, 30px 30px, 30px 30px, auto;
  }

  .hero .am-wrap {
    padding-inline: 16px;
  }

  h1 {
    font-size: 45px;
  }

  .lede {
    margin-bottom: 24px;
  }

  .hero-action {
    flex: 1;
  }

  .install {
    --am-technical-font-size: 12px;
  }

  .product-stage :deep(.details) {
    display: none;
  }

}

</style>
