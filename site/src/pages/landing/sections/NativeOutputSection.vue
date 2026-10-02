<script setup lang="ts">
import { withBase } from 'vitepress'
import { computed, ref } from 'vue'
import { DOCS_PATH } from '../../../shared/site'
import { DEMO_RESOURCE_NAMES } from '../data/demo'
import {
  kinds,
  outputRows,
  targets,
  type Status
} from '../data/harnesses'
import SectionShell from '../components/SectionShell.vue'

const KINDS = kinds.map((kind) => kind.id)
const ownershipPath = withBase(`${DOCS_PATH}explanation/shared-configuration-ownership`)

const EXAMPLE_NAMES = {
  skill: 'tdd',
  command: 'create-module',
  subagent: DEMO_RESOURCE_NAMES.oncall,
  mcp: DEMO_RESOURCE_NAMES.docsSearch,
  hook: 'pre-user-prompt',
  rule: 'project-guide',
}

const STATUS_META: Record<Status, { badge: string; note: string }> = {
  native: { badge: '', note: '' },
  shared: { badge: 'shared file', note: 'An entry inside a file the harness owns.' },
  'as-skill': { badge: 'as skill', note: 'No command primitive, so it ships as a skill.' },
  none: { badge: '', note: '' }
}

const metaFor = (status: Status) => STATUS_META[status] ?? { badge: status, note: '' }

/** Claude Code leads when present: it is the harness most visitors know. */
const active = ref(targets.find((target) => target.id === 'claude')?.id ?? targets[0]?.id ?? '')

const rows = computed(() => outputRows(active.value, KINDS, EXAMPLE_NAMES))
const supportingArtifacts = computed(() =>
  [...new Set(rows.value.flatMap((row) => row.extra))],
)
</script>

<template>
  <SectionShell
    id="harnesses"
    density="compact"
    eyebrow="Native output"
    title="Same resources. Different native layouts"
  >
    <template #sub>
      Each harness expects agent tooling in different paths and formats. <span class="product-name">agent-marketplace</span>
      handles those differences for you.
    </template>

    <div class="tablist" role="tablist">
      <button
        v-for="target in targets"
        :key="target.id"
        class="tab"
        :class="{ 'is-on': target.id === active }"
        role="tab"
        :aria-selected="target.id === active"
        @click="active = target.id"
      >
        {{ target.label }}
      </button>
    </div>

    <div class="pane">
      <div
        v-for="row in rows"
        :key="row.kindId"
        class="row"
        :class="{ 'is-drop': !row.lead }"
        :title="row.codes.join(' · ') || undefined"
      >
        <span class="kind">{{ row.kindLabel }}</span>
        <span class="arrow">→</span>
        <span class="status-mark" :class="{ 'is-none': !row.lead }" aria-hidden="true">
          {{ row.lead ? '✓' : '–' }}
        </span>
        <code>
          {{ row.lead ?? `no ${row.kindLabel.toLowerCase()} primitive` }}
        </code>
        <button
          v-if="metaFor(row.status).badge"
          type="button"
          class="status-tag"
          :aria-label="metaFor(row.status).badge"
          :aria-describedby="`output-note-${row.kindId}`"
        >
          {{ metaFor(row.status).badge }}
          <span :id="`output-note-${row.kindId}`" class="status-tooltip" role="tooltip">
            {{ metaFor(row.status).note }}
          </span>
        </button>
      </div>

      <div v-if="supportingArtifacts.length" class="pane-footer">
        <span>Also writes:</span>
        <code v-for="path in supportingArtifacts" :key="path">{{ path }}</code>
      </div>
    </div>

    <p class="configuration-note">
      <svg aria-hidden="true" viewBox="0 0 24 24">
        <path d="M12 3 20 6v5c0 5-3.4 8.5-8 10-4.6-1.5-8-5-8-10V6z" />
      </svg>
      <span>
        Only entries managed by agent-marketplace are updated. Your other settings stay intact.
        <a :href="ownershipPath">Learn more →</a>
      </span>
    </p>
  </SectionShell>
</template>

<style scoped>
.tablist {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.tab {
  padding: 8px 14px;
  border-radius: var(--am-radius-sm);
  border: 1px solid var(--am-border);
  background-color: var(--am-surface);
  color: var(--vp-c-text-2);
  font-size: var(--am-text-body-sm);
  cursor: pointer;
}

.tab:hover {
  color: var(--vp-c-text-1);
  border-color: var(--am-border-strong);
  background: var(--am-surface-raised);
}

.tab.is-on {
  background-color: var(--vp-c-brand-1);
  border-color: var(--vp-c-brand-1);
  color: var(--am-on-accent);
  font-weight: var(--am-font-semibold);
}

.pane {
  overflow-x: auto;
  padding: 10px 18px 14px;
  border: 1px solid var(--am-code-border);
  border-radius: var(--am-radius-lg);
  background: var(--am-code-bg);
  color: var(--am-code-text);
  font-family: var(--vp-font-family-mono), monospace;
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 43px;
  border-bottom: 1px solid var(--am-code-border);
  font-size: var(--am-text-body-sm);
}

.kind {
  width: 88px;
  flex: none;
  font-weight: var(--am-font-semibold);
  line-height: 1;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.kind,
.arrow,
.pane-footer {
  color: var(--am-code-muted);
}

.kind,
.pane-footer {
  font-size: var(--am-text-supporting);
}

.status-mark {
  flex: 0 0 14px;
  color: var(--vp-c-brand-1);
  font-size: var(--am-text-body-sm);
  font-weight: var(--am-font-bold);
  text-align: center;
}

.status-mark.is-none {
  color: var(--vp-c-text-3);
  font-weight: normal;
}

.row code {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  font-size: var(--am-text-supporting);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row.is-drop code {
  color: var(--am-code-muted);
  font-style: italic;
}

.status-tag {
  position: relative;
  flex: none;
  padding: 3px 6px;
  border: 1px solid var(--am-code-border);
  border-radius: 4px;
  background: var(--am-code-bg-raised);
  color: var(--am-code-muted);
  font-family: inherit;
  font-size: var(--am-text-supporting);
  line-height: 1.2;
  cursor: help;
}

.status-tag:is(:hover, :focus) {
  border-color: var(--am-border-strong);
  color: var(--am-code-text);
}

.status-tag:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 2px;
}

.status-tooltip {
  position: absolute;
  z-index: 5;
  right: 0;
  bottom: calc(100% + 8px);
  display: none;
  width: max-content;
  max-width: min(240px, 70vw);
  padding: 8px 10px;
  border: 1px solid var(--am-terminal-border);
  border-radius: var(--am-radius-sm);
  background: var(--am-terminal-bg-raised);
  color: var(--am-terminal-text);
  font-family: var(--vp-font-family-base), sans-serif;
  font-size: var(--am-text-supporting);
  line-height: 1.4;
  text-align: left;
  white-space: normal;
  pointer-events: none;
}

.status-tag:is(:hover, :focus) .status-tooltip {
  display: block;
}

.pane-footer {
  display: flex;
  min-height: 38px;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px 9px;
  padding-top: 8px;
}

.pane-footer code {
  color: var(--am-code-text);
}

.configuration-note {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin: 18px 0 0;
  color: var(--vp-c-text-2);
  font-size: var(--am-text-supporting);
  line-height: 1.55;
}

.configuration-note svg {
  width: 15px;
  height: 15px;
  flex: 0 0 auto;
  margin-top: 2px;
  fill: none;
  stroke: var(--vp-c-brand-1);
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.7;
}

.configuration-note a {
  color: var(--vp-c-brand-1);
  font-weight: var(--am-font-semibold);
  text-decoration: none;
}

.configuration-note a:hover {
  color: var(--vp-c-brand-2);
  text-decoration: underline;
  text-underline-offset: 3px;
}

.configuration-note a:focus-visible {
  border-radius: 2px;
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 3px;
}

.product-name {
  display: inline-block;
  padding: 2px 6px;
  border: 1px solid var(--am-accent-border);
  border-radius: var(--am-radius-sm);
  background: var(--am-accent-soft);
  color: var(--vp-c-text-1);
  font-family: var(--vp-font-family-mono), monospace;
  font-size: 0.9em;
  line-height: 1.2;
}

@media (max-width: 680px) {
  .tab {
    padding-inline: 10px;
    font-size: var(--am-text-supporting);
  }

  .row {
    gap: 8px;
  }

  .kind {
    width: 74px;
  }
}

</style>
