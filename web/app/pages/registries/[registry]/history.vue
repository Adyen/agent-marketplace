<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { routeParam, registryAsyncDataKey, registryHistoryAsyncDataKey } from '~/lib/routes'
import { MAX_REGISTRY_HISTORY_ENTRIES } from '~/lib/validation'

const route = useRoute()
const api = useMarketplaceApi()
const registry = routeParam(route.params.registry, 'registry')
const heading = ref<HTMLElement | null>(null)

const { data: registryData, error: registryError } = await useAsyncData(
  registryAsyncDataKey(registry),
  () => api.getRegistry(registry),
)
const { data: history, pending, error, refresh } = await useAsyncData(
  registryHistoryAsyncDataKey(registry),
  () => api.getRegistryHistory(registry),
)

watch([pending, history, error], async () => {
  if (pending.value) return
  await nextTick()
  heading.value?.focus()
}, { flush: 'post' })

const overviewPath = `/registries/${encodeURIComponent(registry)}`

function retry() {
  refresh()
}

useHead(() => ({
  title: registryData.value ? `${registryData.value.slug} · Registry history · Agent Marketplace` : 'Registry history · Agent Marketplace',
  meta: [{ name: 'description', content: 'Read-only registry occurrence history with exact commits, refs, acquisition modes, and digests.' }],
}))
</script>

<template>
  <div class="min-h-screen bg-[var(--am-color-bg-canvas)] text-default">
    <main id="main-content" class="mx-auto max-w-6xl px-4 py-6 sm:px-6 sm:py-8 lg:px-8" tabindex="-1">
      <MarketplaceRecordHeader :breadcrumbs="[{ label: 'Marketplace', to: '/' }, { label: registryData?.slug || registry, to: overviewPath }, { label: 'History' }]" />
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-[var(--am-color-bg-surface)] p-8">
        <h1 ref="heading" tabindex="-1" class="text-2xl font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Loading registry history</h1>
        <p class="mt-3 text-sm text-toned">Fetching immutable registry occurrences.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-[var(--am-color-bg-surface)] p-8">
        <h1 ref="heading" tabindex="-1" class="text-2xl font-semibold text-error focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Registry history unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-toned">The Marketplace could not load this history. The registry may be unavailable, or the response may exceed the safety limit of {{ MAX_REGISTRY_HISTORY_ENTRIES }} occurrences. Invalid and oversized responses are not rendered.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <template v-else>
        <div class="mb-8 flex flex-wrap items-end justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-primary">Registry history</p>
            <h1 ref="heading" tabindex="-1" class="mt-1 break-words text-3xl font-semibold tracking-tight text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">{{ registryData?.slug || registry }}</h1>
            <p class="mt-2 text-sm leading-6 text-toned">Each occurrence records the exact registry revision observed by this Marketplace.</p>
          </div>
          <NuxtLink :to="overviewPath" class="text-sm font-medium text-primary underline-offset-4 hover:underline">Back to registry</NuxtLink>
        </div>
        <UAlert v-if="registryError" color="warning" variant="subtle" icon="i-lucide-info" title="Registry metadata is unavailable" description="History is shown from the registry history endpoint, but overview metadata could not be loaded." class="mb-6" />
        <RegistryHistoryList :occurrences="history || []" />
      </template>
    </main>
  </div>
</template>
