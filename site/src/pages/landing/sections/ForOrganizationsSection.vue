<script setup lang="ts">
import { withBase } from 'vitepress'
import { DOCS_PATH } from '../../../shared/site'
import ActionLink from '../components/ActionLink.vue'
import SectionShell from '../components/SectionShell.vue'
import TerminalFrame from '../components/TerminalFrame.vue'

const deploymentGuidePath = withBase(`${DOCS_PATH}how-to/operator/deploy-a-production-marketplace`)

const resources = [
  { name: 'code-review', description: 'Consistent reviews across your team.' },
  { name: 'onboarding', description: "Get started with your team's workflow." },
  { name: 'docs-search', description: 'Find answers in your documentation.' },
]
</script>

<template>
  <SectionShell
    id="organizations"
    class="organizations"
    eyebrow="For organizations"
    title="A marketplace for your team"
  >
    <template #sub>
      Give your team a shared place to discover and install AI agent resources.
    </template>
    <template #action>
      <ActionLink :href="deploymentGuidePath" variant="outline">
        Deployment guide
        <span aria-hidden="true">→</span>
      </ActionLink>
    </template>

    <div class="benefits">
      <article class="benefit">
        <span class="benefit__icon">
          <svg aria-hidden="true" viewBox="0 0 24 24">
            <ellipse cx="12" cy="5" rx="7" ry="3" />
            <path d="M5 5v7c0 1.7 3.1 3 7 3s7-1.3 7-3V5" />
            <path d="M5 12v7c0 1.7 3.1 3 7 3s7-1.3 7-3v-7" />
          </svg>
        </span>
        <div>
          <h3>Your registries, together</h3>
          <p>Bring resources from multiple registries into one place.</p>
        </div>
      </article>

      <article class="benefit">
        <span class="benefit__icon">
          <svg aria-hidden="true" viewBox="0 0 24 24">
            <path d="M7 3h7l4 4v14H7z" />
            <path d="M14 3v5h4M10 14h5M10 17h3" />
          </svg>
        </span>
        <div>
          <h3>Publication policies</h3>
          <p>Define allowed licenses before resources are published.</p>
        </div>
      </article>

      <article class="benefit">
        <span class="benefit__icon">
          <svg aria-hidden="true" viewBox="0 0 24 24">
            <path d="m12 3 8 4.5v9L12 21l-8-4.5v-9z" />
            <path d="m4 7.5 8 4.5 8-4.5M12 12v9" />
          </svg>
        </span>
        <div>
          <h3>Your infrastructure</h3>
          <p>Deploy with Docker Compose or Helm.</p>
        </div>
      </article>
    </div>

    <TerminalFrame
      class="marketplace-window"
      label="Team Marketplace"
      aria-label="Example team Marketplace resource catalog"
    >
      <template #status>
        <span class="preview-status">Preview</span>
      </template>

      <div class="marketplace-window__body">
        <aside class="filters" aria-hidden="true">
          <strong>Kind</strong>
          <span class="filter is-selected"><i />All kinds</span>
          <span class="filter"><i />Skill</span>
          <span class="filter"><i />Command</span>
          <span class="filter"><i />MCP server</span>
        </aside>

        <div class="catalog">
          <div class="search">
            <svg aria-hidden="true" viewBox="0 0 24 24">
              <circle cx="11" cy="11" r="6" />
              <path d="m16 16 4 4" />
            </svg>
            <span>Search resources...</span>
          </div>
          <h3>Resources</h3>
          <article v-for="resource in resources" :key="resource.name" class="resource-row">
            <span class="fingerprint" aria-hidden="true">
              <i /><i /><i /><i /><i /><i /><i /><i /><i />
            </span>
            <div class="resource-row__copy">
              <span>Collection · team</span>
              <strong>{{ resource.name }}</strong>
              <p>{{ resource.description }}</p>
            </div>
            <span class="install-button">
              <svg aria-hidden="true" viewBox="0 0 24 24">
                <rect x="8" y="8" width="11" height="11" rx="2" />
                <path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" />
              </svg>
              Copy install command
            </span>
          </article>
        </div>
      </div>
    </TerminalFrame>
  </SectionShell>
</template>

<style scoped>
.organizations {
  --am-section-heading-width: 100%;
}

.benefits {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 32px;
  margin-bottom: 38px;
}

.benefit {
  display: flex;
  align-items: flex-start;
  gap: 16px;
}

.benefit__icon {
  display: grid;
  width: 52px;
  height: 52px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: var(--am-radius);
  background: var(--am-accent-soft);
  color: var(--vp-c-brand-1);
}

:is(.benefit__icon, .search, .install-button) svg {
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.benefit__icon svg {
  width: 28px;
  height: 28px;
  stroke-width: 1.7;
}

:is(.benefit h3, .catalog > h3) {
  font-size: var(--am-text-body);
  font-weight: var(--am-font-semibold);
}

.benefit h3 {
  margin: 2px 0 5px;
  line-height: 1.35;
}

.benefit p {
  margin: 0;
  font-size: var(--am-text-body-sm);
  line-height: 1.5;
}

.marketplace-window {
  --am-terminal-bg: var(--am-surface);
  --am-terminal-bg-raised: var(--am-surface-raised);
  --am-terminal-border: var(--am-border);
  --am-terminal-font-family: var(--vp-font-family-base), sans-serif;
  --am-terminal-muted: var(--vp-c-text-2);
  --am-terminal-shadow: 0 24px 50px -34px var(--am-code-shadow);
  --am-terminal-text: var(--vp-c-text-1);
}

.preview-status {
  color: var(--am-terminal-muted);
  font-weight: var(--am-font-semibold);
}

:is(.benefit p, .filters strong, .filter, .search, .resource-row__copy > span, .resource-row__copy p) {
  color: var(--vp-c-text-2);
}

:is(.filter, .search, .resource-row__copy strong, .resource-row__copy p) {
  font-size: var(--am-text-supporting);
}

:is(.filters strong, .resource-row__copy > span) {
  font-family: var(--vp-font-family-mono), monospace;
  font-size: 10px;
}

.marketplace-window__body {
  display: grid;
  grid-template-columns: 176px minmax(0, 1fr);
}

.filters {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 24px;
  border-right: 1px solid var(--am-border);
}

.filters strong {
  margin-bottom: 4px;
  font-weight: var(--am-font-semibold);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.filter {
  display: flex;
  align-items: center;
  gap: 10px;
}

.filter i {
  width: 16px;
  height: 16px;
  border: 1px solid var(--am-border-strong);
  border-radius: 50%;
}

.filter.is-selected {
  color: var(--vp-c-text-1);
}

.filter.is-selected i {
  border: 4px solid var(--am-surface);
  background: var(--vp-c-brand-1);
  box-shadow: 0 0 0 1px var(--vp-c-brand-1);
}

.catalog {
  min-width: 0;
  padding: 18px 24px 6px;
}

.search {
  display: flex;
  align-items: center;
  gap: 9px;
  height: 38px;
  padding: 0 12px;
  border: 1px solid var(--am-border-strong);
  border-radius: var(--am-radius-sm);
}

.search svg,
.install-button svg {
  width: 16px;
  height: 16px;
  stroke-width: 1.8;
}

.catalog > h3 {
  margin: 15px 0 5px;
}

.resource-row {
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr) auto;
  gap: 14px;
  align-items: center;
  min-height: 88px;
  border-bottom: 1px solid var(--am-border);
}

.resource-row:last-child {
  border-bottom: 0;
}

.fingerprint {
  display: grid;
  width: 24px;
  height: 24px;
  grid-template-columns: repeat(3, 1fr);
}

.fingerprint i {
  background: var(--vp-c-text-1);
  opacity: 0.16;
}

.fingerprint i:nth-child(2n),
.fingerprint i:nth-child(5) {
  opacity: 0.75;
}

.fingerprint i:nth-child(3n) {
  opacity: 0.38;
}

.resource-row__copy {
  min-width: 0;
}

.resource-row__copy > span {
  display: block;
  margin-bottom: 2px;
}

.resource-row__copy strong {
  display: block;
  line-height: 1.4;
}

.resource-row__copy p {
  margin: 2px 0 0;
  overflow: hidden;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.install-button {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 34px;
  padding: 0 11px;
  border: 1px solid var(--am-border);
  border-radius: var(--am-radius-sm);
  background: var(--am-surface-soft);
  font-size: 11px;
  font-weight: var(--am-font-semibold);
}

@media (max-width: 820px) {
  .benefits {
    grid-template-columns: 1fr;
    gap: 24px;
  }
}

@media (max-width: 680px) {
  .marketplace-window__body {
    grid-template-columns: 1fr;
  }

  :is(.filters, .install-button) {
    display: none;
  }

  .catalog {
    padding: 14px 16px 4px;
  }

  .resource-row {
    grid-template-columns: 24px minmax(0, 1fr);
    min-height: 82px;
  }
}
</style>
