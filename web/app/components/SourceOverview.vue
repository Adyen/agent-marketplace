<script setup lang="ts">
import { computed } from 'vue'
import { safeCanonicalUrl } from '~/lib/presentation'
import type { Marketplace, Source } from '~/types/marketplace'

const props = defineProps<{
  source: Source
  operator?: Marketplace
}>()
const operatorCanonicalUrl = computed(() => safeCanonicalUrl(props.operator?.canonical_url))
</script>

<template>
  <article class="rounded-2xl border border-default bg-default p-6 shadow-sm sm:p-8">
    <div class="flex flex-wrap items-start justify-between gap-4 border-b border-default pb-6">
      <div class="min-w-0">
        <p class="text-sm font-medium text-primary">Source overview</p>
        <h1 class="mt-1 break-words text-3xl font-semibold tracking-tight text-highlighted">{{ source.slug }}</h1>
        <p class="mt-2 break-all font-mono text-xs text-muted">ID: {{ source.id }}</p>
      </div>
      <UBadge :color="source.listed ? 'success' : 'neutral'" variant="subtle">{{ source.listed ? 'Listed' : 'Unlisted' }}</UBadge>
    </div>

    <section class="border-b border-default py-6" aria-labelledby="source-trust-heading">
      <h2 id="source-trust-heading" class="text-lg font-semibold text-highlighted">Publisher and operator</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The publisher owns the source repository. The Marketplace operator approves this source for discovery and distribution; approval is not a safety certification or endorsement.</p>
      <dl class="mt-4 grid gap-4 sm:grid-cols-2">
        <div class="rounded-xl border border-default bg-muted/30 p-4">
          <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Source publisher</dt>
          <dd class="mt-2 break-words text-sm text-highlighted">{{ source.publisher?.name || 'Not provided' }}</dd>
          <dd v-if="source.publisher?.url" class="mt-1 break-all font-mono text-xs text-muted">{{ source.publisher.url }}</dd>
        </div>
        <div class="rounded-xl border border-default bg-muted/30 p-4">
          <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Marketplace operator</dt>
          <dd class="mt-2 break-words text-sm text-highlighted">{{ operator?.name || 'Marketplace instance' }}</dd>
          <dd class="mt-1 break-all font-mono text-xs text-muted">{{ operator?.id || 'Not provided' }}</dd>
          <dd v-if="operatorCanonicalUrl" class="mt-1 break-all font-mono text-xs text-muted">{{ operatorCanonicalUrl }}</dd>
        </div>
      </dl>
    </section>

    <section class="border-b border-default py-6" aria-labelledby="source-details-heading">
      <h2 id="source-details-heading" class="text-lg font-semibold text-highlighted">Acquisition and identity</h2>
      <dl class="mt-4 grid gap-x-6 gap-y-5 sm:grid-cols-2">
        <div>
          <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Acquisition</dt>
          <dd class="mt-1 text-sm text-highlighted">{{ source.acquisition }}</dd>
        </div>
        <div>
          <dt class="text-xs font-semibold uppercase tracking-wide text-muted">License</dt>
          <dd class="mt-1 break-words text-sm text-highlighted">{{ source.license || 'Not provided' }}</dd>
        </div>
        <div class="sm:col-span-2">
          <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Configured ref</dt>
          <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ source.ref }}</dd>
        </div>
        <div class="sm:col-span-2">
          <dt class="text-xs font-semibold uppercase tracking-wide text-muted">Aliases</dt>
          <dd v-if="source.aliases?.length" class="mt-1 break-words text-sm text-highlighted">{{ source.aliases.join(', ') }}</dd>
          <dd v-else class="mt-1 text-sm text-muted">No aliases recorded.</dd>
        </div>
      </dl>
    </section>

    <section class="border-b border-default py-6" aria-labelledby="source-roots-heading">
      <h2 id="source-roots-heading" class="text-lg font-semibold text-highlighted">Source roots</h2>
      <ul v-if="source.roots.length" class="mt-4 divide-y divide-default rounded-xl border border-default">
        <li v-for="(root, index) in source.roots" :key="`${root.path}-${index}`" class="grid gap-1 px-4 py-3 sm:grid-cols-[minmax(8rem,0.35fr)_1fr] sm:gap-4">
          <span class="text-sm font-medium text-muted">Path</span>
          <span class="break-all font-mono text-sm text-highlighted">{{ root.path }}</span>
          <span v-if="root.source_harness" class="text-sm font-medium text-muted">Source harness</span>
          <span v-if="root.source_harness" class="break-words text-sm text-highlighted">{{ root.source_harness }}</span>
        </li>
      </ul>
      <p v-else class="mt-3 text-sm text-muted">No source roots were provided.</p>
    </section>

    <section class="pt-6" aria-labelledby="source-origins-heading">
      <h2 id="source-origins-heading" class="text-lg font-semibold text-highlighted">Current and historical origins</h2>
      <ul v-if="source.origins.length" class="mt-4 space-y-2">
        <li v-for="(origin, index) in source.origins" :key="`${origin}-${index}`" class="break-all rounded-xl border border-default bg-muted/30 px-4 py-3 font-mono text-sm text-highlighted">{{ origin }}</li>
      </ul>
      <p v-else class="mt-3 text-sm text-muted">No origins were provided.</p>
    </section>
  </article>
</template>
