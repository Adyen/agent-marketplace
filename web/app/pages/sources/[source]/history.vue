<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { routeParam, sourceAsyncDataKey, sourceHistoryAsyncDataKey } from '~/lib/routes'
import { MAX_SOURCE_HISTORY_ENTRIES } from '~/lib/validation'

const route = useRoute()
const api = useMarketplaceApi()
const source = routeParam(route.params.source, 'source')
const heading = ref<HTMLElement | null>(null)

const { data: sourceData, error: sourceError } = await useAsyncData(
  sourceAsyncDataKey(source),
  () => api.getSource(source),
)
const { data: history, pending, error, refresh } = await useAsyncData(
  sourceHistoryAsyncDataKey(source),
  () => api.getSourceHistory(source),
)

watch([pending, history, error], async () => {
  if (pending.value) return
  await nextTick()
  heading.value?.focus()
}, { flush: 'post' })

const overviewPath = `/sources/${encodeURIComponent(source)}`

function retry() {
  refresh()
}

useHead(() => ({
  title: sourceData.value ? `${sourceData.value.slug} · Source history · Agent Marketplace` : 'Source history · Agent Marketplace',
  meta: [{ name: 'description', content: 'Read-only source occurrence history with exact commits, refs, acquisition modes, and digests.' }],
}))
</script>

<template>
  <div class="min-h-screen bg-muted/30 text-default">
    <MarketplaceSkipLink target="source-history-content" />
    <MarketplaceRecordHeader :breadcrumbs="[{ label: 'Source', to: overviewPath }, { label: 'History' }]" />

    <main id="source-history-content" class="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8" tabindex="-1">
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-default p-8">
        <h1 ref="heading" tabindex="-1" class="text-2xl font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Loading source history</h1>
        <p class="mt-3 text-sm text-muted">Fetching immutable source occurrences.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-default p-8">
        <h1 ref="heading" tabindex="-1" class="text-2xl font-semibold text-error focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Source history unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-muted">The Marketplace could not load this history. The source may be unavailable, or the response may exceed the safety limit of {{ MAX_SOURCE_HISTORY_ENTRIES }} occurrences. Invalid and oversized responses are not rendered.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <template v-else>
        <div class="mb-8 flex flex-wrap items-end justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-primary">Source history</p>
            <h1 ref="heading" tabindex="-1" class="mt-1 break-words text-3xl font-semibold tracking-tight text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">{{ sourceData?.slug || source }}</h1>
            <p class="mt-2 text-sm leading-6 text-muted">Each occurrence records the exact source revision observed by this Marketplace.</p>
          </div>
          <nav aria-label="Source history actions" class="flex flex-wrap gap-2">
            <UButton :to="overviewPath" color="neutral" variant="outline" size="sm">Source overview</UButton>
            <UButton :to="{ path: '/', query: { source } }" color="neutral" variant="outline" size="sm">Search this source</UButton>
          </nav>
        </div>
        <UAlert v-if="sourceError" color="warning" variant="subtle" icon="i-lucide-info" title="Source metadata is unavailable" description="History is shown from the source history endpoint, but overview metadata could not be loaded." class="mb-6" />
        <SourceHistoryList :occurrences="history || []" />
      </template>
    </main>
  </div>
</template>
