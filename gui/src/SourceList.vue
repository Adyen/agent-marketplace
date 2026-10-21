<script setup lang="ts">
import type { SourceDescription } from './api/client'
import GuiIcon, { type IconName } from './GuiIcon.vue'

defineProps<{ title: string; sources: SourceDescription[]; organization: boolean }>()

const kindTags: Record<SourceDescription['kind'], string> = {
  git: 'GIT',
  directory: 'DIRECTORY',
  marketplace: 'MARKETPLACE',
}

const kindIcons: Record<SourceDescription['kind'], IconName> = {
  directory: 'folder',
  git: 'branch',
  marketplace: 'globe',
}

function meta(source: SourceDescription): string {
  const layout = source.sourceHarness === '' ? '' : `layout ${source.sourceHarness}`
  return [source.ref, source.path, layout].filter((value) => value !== '').join(' · ')
}
</script>

<template>
  <section v-if="sources.length > 0" class="settings__group" :aria-label="title">
    <h3>{{ title }}</h3>
    <p v-if="organization">Managed by your organization; read-only.</p>
    <ul class="source-list">
      <li v-for="source in sources" :key="source.id" class="source-card">
        <GuiIcon :name="kindIcons[source.kind]" class="source-card__icon" />
        <div class="source-card__body">
          <div class="source-card__head">
            <span class="source-card__location">{{ source.location }}</span>
            <span class="source-card__tags">
              <span class="gui-tag">{{ kindTags[source.kind] ?? source.kind }}</span>
              <span v-if="organization" class="gui-tag">ORG</span>
            </span>
          </div>
          <p v-if="meta(source) !== ''" class="source-card__meta">{{ meta(source) }}</p>
        </div>
        <slot name="actions" :source="source" />
      </li>
    </ul>
  </section>
</template>
