<script setup lang="ts">
import type { SourceDescription } from './api/client'

defineProps<{ title: string; sources: SourceDescription[]; organization: boolean }>()

const kindLabels: Record<SourceDescription['kind'], string> = {
  git: 'Git registry',
  directory: 'Directory registry',
  marketplace: 'Marketplace endpoint',
}
</script>

<template>
  <section v-if="sources.length > 0" class="settings__group" :aria-label="title">
    <h3>{{ title }}</h3>
    <p v-if="organization">Managed by your organization; read-only.</p>
    <ul>
      <li v-for="source in sources" :key="source.id" class="settings__source-item">
        <span>{{ kindLabels[source.kind] ?? source.kind }}</span>
        <span class="mono settings__source-location">{{ source.location }}</span>
        <span v-if="source.ref">Ref {{ source.ref }}</span>
        <span v-if="source.path">Subpath {{ source.path }}</span>
        <span v-if="source.sourceHarness">Source layout {{ source.sourceHarness }}</span>
        <slot name="actions" :source="source" />
      </li>
    </ul>
  </section>
</template>
