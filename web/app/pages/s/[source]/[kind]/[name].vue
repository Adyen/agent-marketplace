<script setup lang="ts">
import { computed } from 'vue'
import { isResourceKind } from '~/lib/api'
import { safeCanonicalUrl } from '~/lib/presentation'

const route = useRoute()
const api = useMarketplaceApi()

function routeValue(value: string | string[] | undefined): string {
  const raw = Array.isArray(value) ? value[0] : value
  try {
    return decodeURIComponent(raw || '')
  } catch {
    return ''
  }
}

const source = routeValue(route.params.source)
const kindValue = routeValue(route.params.kind)
const name = routeValue(route.params.name)
if (!isResourceKind(kindValue)) throw createError({ statusCode: 404, statusMessage: 'Resource kind not found' })
const kind = kindValue

const { data: metadata } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: resource, pending, error, refresh } = await useAsyncData(
  `marketplace-resource-${source}-${kind}-${name}`,
  () => api.getResource(source, kind, name),
)
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.canonical_url) || (import.meta.client ? window.location.origin : ''))

function retry() {
  refresh()
}

useHead(() => ({
  title: resource.value ? `${name} · Agent Marketplace` : 'Resource detail · Agent Marketplace',
  meta: [{ name: 'description', content: 'Read-only resource provenance, security, and acquisition details.' }],
}))
</script>

<template>
  <div class="min-h-screen bg-muted/30 text-default">
    <a href="#resource-content" class="sr-only z-50 rounded-lg bg-primary px-4 py-3 text-white focus:not-sr-only focus:fixed focus:left-4 focus:top-4">Skip to content</a>
    <header class="border-b border-default bg-default/90">
      <div class="mx-auto flex max-w-5xl items-center px-4 py-4 sm:px-6 lg:px-8">
        <NuxtLink to="/" class="rounded-lg text-sm font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Agent Marketplace</NuxtLink>
        <span class="mx-3 text-muted" aria-hidden="true">/</span>
        <span class="truncate text-sm text-muted">Resource detail</span>
      </div>
    </header>

    <main id="resource-content" class="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8" tabindex="-1">
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-default p-8">
        <h1 class="text-2xl font-semibold text-highlighted">Loading resource</h1>
        <p class="mt-3 text-sm text-muted">Fetching the latest verified revision and its metadata.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-default p-8">
        <h1 class="text-2xl font-semibold text-error">Resource unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-muted">This resource could not be loaded. The Marketplace may be unavailable or the resource may no longer be listed.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <ResourceDetailView v-else-if="resource" :resource="resource" :source="source" :kind="kind" :name="name" :canonical-url="canonicalUrl" />
    </main>
  </div>
</template>
