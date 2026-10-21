<script setup lang="ts">
import type { RegistryOccurrence } from '~/types/marketplace'

defineProps<{
  occurrences: RegistryOccurrence[]
}>()
</script>

<template>
  <div v-if="occurrences.length" class="relative">
    <ol aria-label="Registry occurrence timeline" class="divide-y divide-default border-y border-default">
      <li v-for="(occurrence, index) in occurrences" :key="`${occurrence.commit}-${occurrence.occurred_at}-${index}`" class="relative px-1 py-6">
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
            <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Registry revision manifest digest</dt>
            <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ occurrence.registry_revision_manifest_digest }}</dd>
          </div>
          <div v-if="occurrence.mirrored_artifact_digest" class="sm:col-span-2">
            <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Mirrored artifact digest</dt>
            <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ occurrence.mirrored_artifact_digest }}</dd>
          </div>
        </dl>
      </li>
    </ol>
  </div>
  <p v-else class="border-y border-dashed border-default py-12 text-center text-sm text-toned">No registry occurrences have been recorded.</p>
</template>
