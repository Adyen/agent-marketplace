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
      <ActionLink
        :href="deploymentGuidePath"
        label="Deployment guide"
        variant="text"
        icon="arrow"
      />
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
          <p>Run the Marketplace in the environment your team already operates.</p>
        </div>
      </article>
    </div>

    <TerminalFrame
      class="marketplace-window is-titled"
      label="Team Marketplace"
      aria-label="Example team Marketplace resource catalog"
    >
      <template #status>
        <span class="preview-status am-outline-chip">Preview</span>
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
            <svg class="am-fill-icon" aria-hidden="true" viewBox="0 0 24 24">
              <path
                fill-rule="evenodd"
                d="M10.5 3a7.5 7.5 0 1 0 4.7 13.35l4.7 4.7 1.15-1.15-4.7-4.7A7.5 7.5 0 0 0 10.5 3Zm0 2.5a5 5 0 1 0 0 10 5 5 0 0 0 0-10Z"
                clip-rule="evenodd"
              />
            </svg>
            <span>Search resources...</span>
          </div>
          <h3>Resources</h3>
          <article v-for="resource in resources" :key="resource.name" class="resource-row">
            <span class="fingerprint" aria-hidden="true">
              <i /><i /><i /><i /><i /><i /><i /><i /><i />
            </span>
            <div class="resource-row__copy">
              <span class="resource-row__title">
                <span class="resource-kind">Collection</span>
                <span class="resource-separator" aria-hidden="true">·</span>
                <strong>{{ resource.name }}</strong>
              </span>
              <p>{{ resource.description }}</p>
            </div>
            <span class="install-button">
              <svg class="am-fill-icon" aria-hidden="true" viewBox="0 0 24 24">
                <path d="M16 1H4a2 2 0 0 0-2 2v14h2V3h12V1Zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2Zm0 16H8V7h11v14Z" />
              </svg>
              Copy
            </span>
          </article>
        </div>
      </div>
    </TerminalFrame>
  </SectionShell>
</template>

<style scoped>
@media (min-width: 960px) {
  .organizations {
    --lp-section-pad: var(--lp-space-group);

    min-height: calc(100svh - var(--lp-nav-height));
  }

  .organizations :deep(.head) {
    margin-bottom: var(--lp-space-group);
  }
}

.benefits {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  column-gap: var(--lp-gutter);
  margin-bottom: var(--lp-space-default);
}

.benefit {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: var(--lp-space-default);
  padding-left: var(--lp-space-default);
  box-shadow: inset 1px 0 0 var(--lp-outline);
}

.benefit__icon {
  display: block;
  width: var(--lp-space-default);
  height: var(--lp-space-default);
  color: var(--lp-text);
}

.benefit__icon svg {
  width: 100%;
  height: 100%;
  fill: none;
  stroke: currentcolor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.5;
}

.benefit h3,
.catalog > h3 {
  margin: 0;
  color: var(--lp-text);
  font: var(--lp-font-heading);
}

.benefit p {
  max-width: var(--lp-measure);
  margin: var(--lp-space-tight) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-body);
  text-wrap: pretty;
}

.marketplace-window__body {
  display: grid;
  font: var(--lp-font-depiction);
  grid-template-columns: 12rem minmax(0, 1fr);
}

.filters {
  display: flex;
  flex-direction: column;
  gap: var(--lp-space-related);
  padding: var(--lp-inset);
  border-right: 1px solid var(--lp-divider);
}

.filters strong {
  color: var(--lp-text);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.filter {
  color: var(--lp-text-2);
}

.filter.is-selected {
  color: var(--lp-text);
}

.filter i {
  display: none;
}

.catalog {
  min-width: 0;
  padding: var(--lp-inset);
}

.search {
  display: flex;
  height: var(--lp-h-depiction);
  align-items: center;
  gap: var(--lp-space-tight);
  padding: 0 var(--lp-space-related);
  border-radius: var(--lp-radius-control);
  box-shadow: var(--lp-outline-shadow);
  color: var(--lp-text-2);
}

.catalog > h3 {
  margin: var(--lp-space-related) 0 0;
  padding-bottom: var(--lp-space-related);
  font: var(--lp-font-depiction);
}

.resource-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--lp-space-related);
  padding: var(--lp-space-related) 0;
  border-top: 1px solid var(--lp-divider);
}

.fingerprint {
  display: grid;
  width: 1.5rem;
  height: 1.5rem;
  grid-template-columns: repeat(3, 1fr);
  gap: 3px;
}

.fingerprint i {
  background: var(--lp-text);
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

.resource-row__title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: var(--lp-space-tight);
}

.resource-row__copy strong {
  min-width: 0;
  overflow: hidden;
  color: var(--lp-text);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.resource-kind {
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  white-space: nowrap;
}

.resource-separator {
  color: var(--lp-text-2);
  font: var(--lp-font-label);
}

.resource-row__copy p {
  margin: var(--lp-space-tight) 0 0;
  overflow: hidden;
  color: var(--lp-text-2);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.install-button {
  display: flex;
  height: var(--lp-h-depiction);
  align-items: center;
  gap: var(--lp-space-tight);
  padding: 0 var(--lp-space-related);
  border-radius: var(--lp-radius-control);
  box-shadow: var(--lp-outline-shadow);
  color: var(--lp-text);
  font: var(--lp-font-label);
  text-transform: uppercase;
  white-space: nowrap;
}

@media (max-width: 959px) {
  .benefits {
    grid-template-columns: 1fr;
    row-gap: var(--lp-space-group);
  }

  .marketplace-window__body {
    grid-template-columns: 1fr;
  }

  .filters {
    border-right: 0;
    border-bottom: 1px solid var(--lp-divider);
  }
}

@media (max-width: 639px) {
  .install-button {
    display: none;
  }

  .resource-row {
    grid-template-columns: auto minmax(0, 1fr);
  }
}

@media (min-width: 960px) and (max-height: 800px) {
  .organizations {
    --lp-section-pad: var(--lp-space-related);
  }
}
</style>
