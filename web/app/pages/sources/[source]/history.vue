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
    <a href="#source-history-content" class="sr-only z-50 rounded-lg bg-primary px-4 py-3 text-white focus:not-sr-only focus:fixed focus:left-4 focus:top-4">Skip to content</a>
    <header class="border-b border-default bg-default/90 backdrop-blur">
      <div class="mx-auto flex max-w-5xl items-center gap-3 px-4 py-4 sm:px-6 lg:px-8">
        <NuxtLink to="/" class="rounded-lg text-sm font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Agent Marketplace</NuxtLink>
        <span class="text-muted" aria-hidden="true">/</span>
        <NuxtLink :to="overviewPath" class="truncate text-sm text-muted underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Source</NuxtLink>
        <span class="text-muted" aria-hidden="true">/</span>
        <span class="truncate text-sm text-muted">History</span>
        <GitHubLink class="ml-auto" />
      </div>
    </header>

    <main id="source-history-content" class="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8" tabindex="-1">
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-default p-8">
        <h1 ref="heading" tabindex="-1" class="text-2xl font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Loading source history</h1>
        <p class="mt-3 text-sm text-muted">Fetching immutable source occurrences.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-default p-8">
        <h1 ref="heading" tabindex="-1" class="text-2xl font-semibold text-error focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Source history unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-muted">This source history could not be loaded. The Marketplace may be unavailable, the source may no longer be listed, or the response may have exceeded the client safety limit of {{ MAX_SOURCE_HISTORY_ENTRIES }} occurrences. Invalid or oversized responses are not rendered.</p>
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
            <NuxtLink :to="overviewPath" class="inline-flex items-center rounded-lg border border-default bg-default px-3 py-2 text-sm font-medium text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary hover:bg-muted motion-reduce:transition-none">Source overview</NuxtLink>
            <NuxtLink :to="{ path: '/', query: { source } }" class="inline-flex items-center rounded-lg border border-default bg-default px-3 py-2 text-sm font-medium text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary hover:bg-muted motion-reduce:transition-none">Search this source</NuxtLink>
          </nav>
        </div>
        <UAlert v-if="sourceError" color="warning" variant="subtle" icon="i-lucide-info" title="Source metadata is unavailable" description="History is shown from the source history endpoint, but overview metadata could not be loaded." class="mb-6" />
        <SourceHistoryList :occurrences="history || []" />
      </template>
    </main>
  </div>
</template>
