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
import LandingTabList from '../components/LandingTabList.vue'
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
    eyebrow="Native output"
    title="Same resources. Different native layouts"
  >
    <template #sub>
      Each harness expects agent tooling in different paths and formats. <span class="product-name">agent-marketplace</span>
      handles those differences for you.
    </template>

    <LandingTabList
      v-model="active"
      class="native-tabs"
      id-prefix="native-output"
      label="Harness target"
      :tabs="targets"
    />

    <div
      :id="`native-output-panel-${active}`"
      class="pane"
      role="tabpanel"
      :aria-labelledby="`native-output-tab-${active}`"
    >
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
          class="status-tag am-outline-chip am-hit-target"
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
      <svg class="am-stroke-icon" aria-hidden="true" viewBox="0 0 24 24">
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
.native-tabs {
  margin-bottom: var(--lp-space-default);
}

.row {
  display: grid;
  grid-template-columns: 6rem var(--lp-inset) var(--lp-inset) minmax(0, 1fr) auto;
  align-items: baseline;
  column-gap: var(--lp-space-related);
  padding: var(--lp-space-related) 0;
  border-top: 1px solid var(--lp-divider);
}

.pane-footer {
  border-top: 1px solid var(--lp-divider);
}

.row:last-child,
.pane-footer:last-child {
  border-bottom: 1px solid var(--lp-divider);
}

.kind {
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.arrow {
  color: var(--lp-text-muted);
  font: var(--lp-font-label);
  text-align: center;
}

.status-mark {
  display: grid;
  width: var(--lp-inset);
  height: var(--lp-inset);
  align-self: center;
  place-items: center;
  border-radius: var(--lp-radius-mark);
  background: var(--lp-green);
  color: var(--lp-on-green);
  font: var(--lp-font-symbol);
}

.status-mark.is-none {
  background: none;
  box-shadow: var(--lp-outline-shadow);
  color: var(--lp-text-2);
}

.row code,
.pane-footer code {
  min-width: 0;
  padding-block: 0.25rem;
  margin-block: -0.25rem;
  overflow-x: auto;
  color: var(--lp-text);
  font: var(--lp-font-label);
  scrollbar-color: var(--lp-outline) transparent;
  scrollbar-width: thin;
  white-space: nowrap;
}

.row.is-drop :is(.kind, code) {
  color: var(--lp-text-2);
}

.status-tag {
  position: relative;
  justify-self: end;
  border: 0;
  background: none;
  cursor: help;
  transition: color var(--lp-motion-feedback) var(--lp-ease);
}

.status-tag:is(:hover, :focus-visible) {
  color: var(--lp-text);
}

.status-tooltip {
  position: absolute;
  z-index: 5;
  right: 0;
  bottom: calc(100% + var(--lp-space-tight));
  width: max-content;
  max-width: min(15rem, 70vw);
  padding: var(--lp-space-related);
  border-radius: var(--lp-radius-control);
  background: var(--lp-fill-solid);
  color: var(--lp-text);
  font: var(--lp-font-caption);
  text-align: left;
  text-transform: none;
  text-wrap: pretty;
  white-space: normal;
  opacity: 0;
  pointer-events: none;
  visibility: hidden;
  transition:
    opacity var(--lp-motion-feedback) var(--lp-ease),
    visibility var(--lp-motion-feedback);
}

.status-tag:is(:hover, :focus-visible) .status-tooltip {
  opacity: 1;
  visibility: visible;
}

.pane-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--lp-space-related);
  padding: var(--lp-space-related) 0;
  font: var(--lp-font-label);
}

.pane-footer > span {
  color: var(--lp-text-2);
  text-transform: uppercase;
}

.configuration-note {
  display: flex;
  align-items: flex-start;
  gap: var(--lp-space-tight);
  margin: var(--lp-space-default) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-caption);
}

.configuration-note svg {
}

.configuration-note a {
  color: var(--lp-text);
  text-decoration: underline;
  text-decoration-skip-ink: none;
  text-decoration-thickness: 1px;
  text-underline-offset: 3px;
}

.product-name {
  white-space: nowrap;
}

@media (max-width: 959px) {
  .row {
    grid-template-columns: 4.5rem var(--lp-inset) var(--lp-inset) minmax(0, 1fr);
    row-gap: var(--lp-space-tight);
  }

  .status-tag {
    grid-column: 4;
    justify-self: start;
  }

}

@media (max-width: 639px) {
  .row {
    grid-template-columns: var(--lp-inset) var(--lp-inset) minmax(0, 1fr);
  }

  .kind {
    grid-column: 1 / -1;
  }

  .status-tag {
    grid-column: 3;
  }
}

@media (prefers-reduced-motion: reduce) {
  .status-tag,
  .status-tooltip {
    transition: none;
  }
}
</style>
