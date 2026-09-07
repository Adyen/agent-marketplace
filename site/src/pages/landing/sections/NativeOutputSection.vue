<script setup lang="ts">
import { computed, ref } from 'vue'
import { DEMO_RESOURCE_NAMES } from '../data/demo'
import {
  kinds,
  outputRows,
  statusesUsed,
  targets,
  type Status
} from '../data/harnesses'
import SectionShell from '../components/SectionShell.vue'

const KINDS = kinds.map((kind) => kind.id)

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
  shared: { badge: 'shared file', note: 'an entry inside a file the harness owns' },
  'as-skill': { badge: 'as skill', note: 'no command primitive, so it ships as a skill' },
  none: { badge: '', note: '' }
}

const metaFor = (status: Status) => STATUS_META[status] ?? { badge: status, note: '' }

/** Claude Code leads when present: it is the harness most visitors know. */
const active = ref(targets.find((target) => target.id === 'claude')?.id ?? targets[0]?.id ?? '')

const rows = computed(() => outputRows(active.value, KINDS, EXAMPLE_NAMES))
const supportingArtifacts = computed(() =>
  [...new Set(rows.value.flatMap((row) => row.extra))],
)

/** Explain only the output forms reached by the full matrix. */
const legend = computed(() => {
  const used = new Set(statusesUsed(KINDS))
  return (Object.keys(STATUS_META) as Status[])
    .filter((status) => used.has(status) && STATUS_META[status].note)
    .map((status) => ({ status, ...STATUS_META[status] }))
})
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
        <span v-if="metaFor(row.status).badge" class="status-tag">
          {{ metaFor(row.status).badge }}
        </span>
      </div>
    </div>

    <p v-if="supportingArtifacts.length" class="supporting-artifacts">
      <span>Also writes:</span>
      <code v-for="path in supportingArtifacts" :key="path">+ {{ path }}</code>
    </p>

    <p v-if="legend.length" class="foot">
      <template v-for="(item, i) in legend" :key="item.status">
        <template v-if="i"> · </template><span class="note-tag">{{ item.badge }}</span> — {{ item.note }}
      </template>
    </p>
  </SectionShell>
</template>

<style scoped>
.tablist {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}

.tab {
  padding: 8px 14px;
  border-radius: var(--am-radius-sm);
  border: 1px solid var(--am-border);
  background-color: var(--am-surface);
  color: var(--vp-c-text-2);
  font-size: 14px;
  cursor: pointer;
  transition:
    color 0.14s,
    border-color 0.14s,
    background-color 0.14s,
    transform 0.14s;
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
  font-weight: 650;
  transform: translateY(-1px);
}

.pane {
  overflow-x: auto;
  padding: 16px 22px 20px;
  border: 1px solid var(--am-code-border);
  border-radius: var(--am-radius-lg);
  background: var(--am-code-bg);
  box-shadow:
    0 24px 50px -34px var(--am-code-shadow),
    inset 0 1px rgba(255, 255, 255, 0.025);
  color: var(--am-code-text);
  font-family: var(--vp-font-family-mono);
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 48px;
  border-bottom: 1px solid var(--am-code-border);
  font-size: 14px;
}

.kind {
  width: 88px;
  flex: none;
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
  font-weight: 600;
  line-height: 1;
  text-transform: uppercase;
  color: var(--am-code-muted);
  letter-spacing: 0.06em;
}

.arrow {
  flex: none;
  color: var(--am-code-muted);
  font-size: 14px;
}

.status-mark {
  width: 14px;
  flex: none;
  color: var(--vp-c-brand-1);
  font-size: 14px;
  font-weight: 700;
  text-align: center;
}

.status-mark.is-none {
  color: var(--vp-c-text-3);
  font-weight: 400;
}

.row code {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--am-code-text);
  font-family: var(--vp-font-family-mono);
  font-size: 13.5px;
  line-height: 1.7;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row.is-drop code {
  color: var(--am-code-muted);
  font-style: italic;
}

.note-tag,
.status-tag {
  padding: 2px 6px;
  border: 1px solid var(--am-code-border);
  border-radius: 4px;
  background: var(--am-code-bg-raised);
  color: var(--am-code-muted);
  font-family: var(--vp-font-family-mono);
  font-size: 12px;
  line-height: 1.2;
}

.note-tag {
  display: inline-block;
}

.status-tag {
  flex: none;
  padding-block: 3px;
}

.foot {
  margin: 16px 0 0;
  font-size: 13.5px;
  color: var(--vp-c-text-2);
}

.supporting-artifacts {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px;
  margin: 14px 0 0;
  color: var(--vp-c-text-2);
  font-size: 13.5px;
}

.supporting-artifacts code {
  color: var(--vp-c-text-1);
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
}

.product-name {
  display: inline-block;
  padding: 2px 6px;
  border: 1px solid var(--am-accent-border);
  border-radius: var(--am-radius-sm);
  background: var(--am-accent-soft);
  color: var(--vp-c-text-1);
  font-family: var(--vp-font-family-mono);
  font-size: 0.9em;
  line-height: 1.2;
}

</style>
