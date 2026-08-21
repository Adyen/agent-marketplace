<script setup lang="ts">
import type { SourceOccurrence } from '~/types/marketplace'

defineProps<{
  occurrences: SourceOccurrence[]
}>()
</script>

<template>
  <div v-if="occurrences.length" class="relative" aria-label="Source occurrence timeline">
    <ol class="space-y-4">
      <li v-for="(occurrence, index) in occurrences" :key="`${occurrence.commit}-${occurrence.occurred_at}-${index}`" class="relative rounded-2xl border border-default bg-default p-5 shadow-sm">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-highlighted">Occurrence {{ occurrences.length - index }}</p>
            <time class="mt-1 block text-sm text-muted" :datetime="occurrence.occurred_at">{{ occurrence.occurred_at }}</time>
          </div>
          <UBadge color="neutral" variant="subtle">{{ occurrence.acquisition }}</UBadge>
        </div>
        <dl class="mt-5 grid gap-x-6 gap-y-4 border-t border-default pt-4 sm:grid-cols-2">
          <div>
            <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Commit</dt>
            <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ occurrence.commit }}</dd>
          </div>
          <div>
            <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Ref</dt>
            <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ occurrence.ref }}</dd>
          </div>
          <div class="sm:col-span-2">
            <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Source revision manifest digest</dt>
            <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ occurrence.source_revision_manifest_digest }}</dd>
          </div>
          <div v-if="occurrence.mirrored_artifact_digest" class="sm:col-span-2">
            <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Mirrored artifact digest</dt>
            <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ occurrence.mirrored_artifact_digest }}</dd>
          </div>
        </dl>
      </li>
    </ol>
  </div>
  <p v-else class="rounded-2xl border border-dashed border-default bg-default p-8 text-sm text-muted">No source occurrences have been recorded.</p>
</template>
