<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { routeParam, registryAsyncDataKey } from '~/lib/routes'

const route = useRoute()
const api = useMarketplaceApi()
const registry = routeParam(route.params.registry, 'registry')
const mainContent = ref<HTMLElement | null>(null)

const { data: registryData, pending, error, refresh } = await useAsyncData(
  registryAsyncDataKey(registry),
  () => api.getRegistry(registry),
)
const { data: operator, error: operatorError } = await useAsyncData('marketplace-instance', () => api.getInstance())

watch([pending, registryData, error], async () => {
  if (pending.value) return
  await nextTick()
  mainContent.value?.focus()
}, { flush: 'post' })

const historyPath = `/registries/${encodeURIComponent(registry)}/history`

function retry() {
  refresh()
}

useHead(() => ({
  title: registryData.value ? `${registryData.value.slug} · Registry · Agent Marketplace` : 'Registry · Agent Marketplace',
  meta: [{ name: 'description', content: 'Read-only registry identity, provenance, acquisition, and origin history.' }],
}))
</script>

<template>
  <NuxtPage v-if="route.path === historyPath" />
  <div v-else class="min-h-screen bg-[var(--am-color-bg-canvas)] text-default">
    <MarketplaceRecordHeader :breadcrumbs="[{ label: 'Registry' }]" />

    <main id="main-content" ref="mainContent" class="mx-auto max-w-5xl rounded px-4 py-10 focus-visible:outline-2 focus-visible:outline-primary sm:px-6 lg:px-8" tabindex="-1">
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-[var(--am-color-bg-surface)] p-8">
        <h1 tabindex="-1" class="text-2xl font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Loading registry</h1>
        <p class="mt-3 text-sm text-toned">Fetching registry identity and acquisition metadata.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-[var(--am-color-bg-surface)] p-8">
        <h1 tabindex="-1" class="text-2xl font-semibold text-error focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Registry unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-toned">This registry could not be loaded. It may no longer be listed or the Marketplace may be unavailable.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <template v-else-if="registryData">
        <div class="mb-6 flex flex-wrap items-center justify-end gap-3">
          <nav aria-label="Registry actions" class="flex flex-wrap gap-2">
            <UButton :to="historyPath" color="neutral" variant="outline" size="sm">View registry history</UButton>
            <UButton :to="{ path: '/', query: { registry } }" color="neutral" variant="outline" size="sm">Search this registry</UButton>
          </nav>
        </div>
        <UAlert v-if="operatorError" color="warning" variant="subtle" icon="i-lucide-info" title="Operator metadata is unavailable" description="The registry is available, but Marketplace operator details could not be loaded." class="mb-6" />
        <RegistryOverview :registry="registryData" :operator="operator" />
      </template>
      <section v-else role="status" class="rounded-2xl border border-dashed border-default bg-[var(--am-color-bg-surface)] p-8">
        <h1 tabindex="-1" class="text-2xl font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Registry not found</h1>
        <p class="mt-3 text-sm text-toned">No registry metadata was returned for this route.</p>
      </section>
    </main>
  </div>
</template>
