<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { routeParam, sourceAsyncDataKey } from '~/lib/routes'

const route = useRoute()
const api = useMarketplaceApi()
const source = routeParam(route.params.source, 'source')
const mainContent = ref<HTMLElement | null>(null)

const { data: sourceData, pending, error, refresh } = await useAsyncData(
  sourceAsyncDataKey(source),
  () => api.getSource(source),
)
const { data: operator, error: operatorError } = await useAsyncData('marketplace-instance', () => api.getInstance())

watch([pending, sourceData, error], async () => {
  if (pending.value) return
  await nextTick()
  mainContent.value?.focus()
}, { flush: 'post' })

const historyPath = `/sources/${encodeURIComponent(source)}/history`

function retry() {
  refresh()
}

useHead(() => ({
  title: sourceData.value ? `${sourceData.value.slug} · Source · Agent Marketplace` : 'Source · Agent Marketplace',
  meta: [{ name: 'description', content: 'Read-only source identity, provenance, acquisition, and origin history.' }],
}))
</script>

<template>
  <div class="min-h-screen bg-muted/30 text-default">
    <a href="#source-content" class="sr-only z-50 rounded-lg bg-primary px-4 py-3 text-white focus:not-sr-only focus:fixed focus:left-4 focus:top-4">Skip to content</a>
    <header class="border-b border-default bg-default/90 backdrop-blur">
      <div class="mx-auto flex max-w-5xl items-center gap-3 px-4 py-4 sm:px-6 lg:px-8">
        <NuxtLink to="/" class="rounded-lg text-sm font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Agent Marketplace</NuxtLink>
        <span class="text-muted" aria-hidden="true">/</span>
        <span class="truncate text-sm text-muted">Source</span>
      </div>
    </header>

    <main id="source-content" ref="mainContent" class="mx-auto max-w-5xl rounded focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary px-4 py-10 sm:px-6 lg:px-8" tabindex="-1">
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-default p-8">
        <h1 tabindex="-1" class="text-2xl font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Loading source</h1>
        <p class="mt-3 text-sm text-muted">Fetching source identity and acquisition metadata.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-default p-8">
        <h1 tabindex="-1" class="text-2xl font-semibold text-error focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Source unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-muted">This source could not be loaded. It may no longer be listed or the Marketplace may be unavailable.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <template v-else-if="sourceData">
        <div class="mb-6 flex flex-wrap items-center justify-end gap-3">
          <nav aria-label="Source actions" class="flex flex-wrap gap-2">
            <NuxtLink :to="historyPath" class="inline-flex items-center rounded-lg border border-default bg-default px-3 py-2 text-sm font-medium text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary hover:bg-muted motion-reduce:transition-none">
              View source history
            </NuxtLink>
            <NuxtLink :to="{ path: '/', query: { source } }" class="inline-flex items-center rounded-lg border border-default bg-default px-3 py-2 text-sm font-medium text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary hover:bg-muted motion-reduce:transition-none">
              Search this source
            </NuxtLink>
          </nav>
        </div>
        <UAlert v-if="operatorError" color="warning" variant="subtle" icon="i-lucide-info" title="Operator metadata is unavailable" description="The source is available, but Marketplace operator details could not be loaded." class="mb-6" />
        <SourceOverview :source="sourceData" :operator="operator" />
      </template>
      <section v-else role="status" class="rounded-2xl border border-dashed border-default bg-default p-8">
        <h1 tabindex="-1" class="text-2xl font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Source not found</h1>
        <p class="mt-3 text-sm text-muted">No source metadata was returned for this route.</p>
      </section>
    </main>
  </div>
</template>
