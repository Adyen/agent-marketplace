<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { SourceDescription } from './api/client'
import GuiIcon, { type IconName } from './GuiIcon.vue'
import type { SourceGroupEntry } from './source-groups'

const props = defineProps<{
  title: string
  entries?: SourceGroupEntry[]
  sources?: SourceDescription[]
  sourceCount?: number
  organization: boolean
  revealRequest?: { sourceId: string; sequence: number } | null
  highlightedSourceId?: string
}>()

const kindIcons: Record<SourceDescription['kind'], IconName> = {
  directory: 'folder',
  git: 'branch',
  marketplace: 'globe',
}

type SourceListRow =
  | { type: 'group'; entry: Extract<SourceGroupEntry, { type: 'group' }> }
  | { type: 'source'; source: SourceDescription; groupKey: string | null }

const root = ref<HTMLElement | null>(null)
const foldedGroups = ref(new Set<string>())
let lastFocusedRevealSequence = 0
const entries = computed(
  () =>
    props.entries ??
    (props.sources ?? []).map((source) => ({
      type: 'source' as const,
      key: source.id,
      layer: props.organization ? ('organization' as const) : ('user' as const),
      source,
    })),
)
const hasSources = computed(() => (props.sourceCount ?? entries.value.length) > 0)
const rows = computed<SourceListRow[]>(() =>
  entries.value.flatMap<SourceListRow>((entry) =>
    entry.type === 'source'
      ? [{ type: 'source' as const, source: entry.source, groupKey: null }]
      : [
          { type: 'group' as const, entry },
          ...(foldedGroups.value.has(entry.key)
            ? []
            : entry.sources.map((source) => ({ type: 'source' as const, source, groupKey: entry.key }))),
        ],
  ),
)

function meta(source: SourceDescription, grouped: boolean): string {
  const layout = source.sourceHarness === '' ? '' : `layout ${source.sourceHarness}`
  return [source.ref, ...(grouped ? [] : [source.path]), layout].filter((value) => value !== '').join(' · ')
}

function toggleGroup(key: string) {
  foldedGroups.value.has(key) ? foldedGroups.value.delete(key) : foldedGroups.value.add(key)
}

watch(
  () => props.revealRequest?.sequence,
  async (sequence) => {
    const request = props.revealRequest
    if (sequence == null || request == null) {
      return
    }
    if (request.sequence <= lastFocusedRevealSequence) {
      return
    }
    const group = entries.value.find(
      (entry) => entry.type === 'group' && entry.sources.some((source) => source.id === request.sourceId),
    )
    if (group?.type === 'group' && foldedGroups.value.has(group.key)) {
      foldedGroups.value.delete(group.key)
      await nextTick()
    }
    await new Promise<void>((resolve) => window.setTimeout(resolve, 0))
    if (request.sequence <= lastFocusedRevealSequence) {
      return
    }
    const card = Array.from(root.value?.querySelectorAll<HTMLElement>('[data-source-id]') ?? []).find(
      (element) => element.dataset.sourceId === request.sourceId,
    )
    if (card == null) {
      return
    }
    lastFocusedRevealSequence = request.sequence
    card?.scrollIntoView?.({ block: 'center' })
    card?.focus({ preventScroll: true })
  },
  { flush: 'post', immediate: true },
)
</script>

<template>
  <section v-if="hasSources" ref="root" class="settings__group" :aria-label="title">
    <h3>{{ title }}</h3>
    <p v-if="organization">Managed by your organization; read-only.</p>
    <ul class="source-list">
      <li v-for="row in rows" :key="row.type === 'group' ? row.entry.key : row.source.id">
        <button
          v-if="row.type === 'group'"
          type="button"
          class="gui-nav-row gui-nav-row--compact"
          :aria-label="`Source group ${row.entry.location}`"
          :aria-expanded="!foldedGroups.has(row.entry.key)"
          @click="toggleGroup(row.entry.key)"
          @keydown.enter.prevent="toggleGroup(row.entry.key)"
          @keydown.space.prevent="toggleGroup(row.entry.key)"
        >
          <span class="source-card__location">{{ row.entry.location }}</span>
          <span class="gui-nav-row__count">{{ row.entry.sources.length }}</span>
        </button>
        <div
          v-else
          class="source-card"
          :class="{
            'source-card--grouped': row.groupKey != null,
            'source-card--highlighted': highlightedSourceId === row.source.id,
          }"
          :data-source-id="row.source.id"
          :data-highlighted="highlightedSourceId === row.source.id ? 'true' : undefined"
          :aria-current="highlightedSourceId === row.source.id ? 'location' : undefined"
          tabindex="-1"
        >
          <GuiIcon :name="kindIcons[row.source.kind]" class="source-card__icon" />
          <div class="source-card__body">
            <div class="source-card__head">
              <span class="source-card__location">{{ row.groupKey == null ? row.source.location : row.source.path || '/' }}</span>
              <span class="source-card__tags">
                <span class="gui-tag">{{ row.source.kind.toUpperCase() }}</span>
                <span v-if="organization" class="gui-tag">ORG</span>
              </span>
            </div>
            <p v-if="meta(row.source, row.groupKey != null) !== ''" class="source-card__meta">{{ meta(row.source, row.groupKey != null) }}</p>
          </div>
          <slot name="actions" :source="row.source" />
        </div>
      </li>
    </ul>
  </section>
</template>
