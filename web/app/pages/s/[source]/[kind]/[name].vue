<script setup lang="ts">
import { computed } from 'vue'
import { isResourceKind } from '~/lib/api'
import { safeCanonicalUrl } from '~/lib/presentation'
import { resourceAsyncDataKey, routeParam } from '~/lib/routes'
import type { InspectionFile } from '~/types/marketplace'

const route = useRoute()
const api = useMarketplaceApi()
const runtimeConfig = useRuntimeConfig()

const source = routeParam(route.params.source, 'source')
const kindValue = routeParam(route.params.kind, 'resource kind')
const name = routeParam(route.params.name, 'resource name')
if (!isResourceKind(kindValue)) throw createError({ statusCode: 404, statusMessage: 'Resource kind not found' })
const kind = kindValue

const { data: metadata } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: resource, pending, error, refresh } = await useAsyncData(
  resourceAsyncDataKey(source, kind, name),
  () => api.getResource(source, kind, name),
)
const { data: revisions, error: revisionsError } = await useAsyncData(
  `${resourceAsyncDataKey(source, kind, name)}:revisions`,
  () => api.listResourceRevisions(source, kind, name),
)
const revision = computed(() => resource.value?.content_digest ?? '')
const { data: inspection } = await useAsyncData(
  `marketplace-inspection:${source}:${kind}:${name}:${revision.value}`,
  () => api.getResourceInspection(source, kind, name, revision.value),
  { immediate: Boolean(revision.value) },
)
const { data: primaryContent } = await useAsyncData<string | undefined>(
  `marketplace-inspection-content:${source}:${kind}:${name}:${revision.value}`,
  () => {
    const primary = inspection.value?.files.find(file => file.path === inspection.value?.primary_path)
    if (!primary || primary.inspection_class === 'image' || primary.inspection_class === 'metadata') return Promise.resolve(undefined)
    return api.getResourceInspectionContent(source, kind, name, revision.value, primary.media_type) as Promise<string>
  },
  { immediate: Boolean(revision.value) },
)
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.browser_base))

function loadInspectionFile(file: InspectionFile): Promise<string | Blob> {
  return api.getResourceInspectionFile(source, kind, name, revision.value, file.content_digest, file.media_type)
}

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
      <ResourceDetailView v-else-if="resource" :resource="resource" :source="source" :kind="kind" :name="name" :canonical-url="canonicalUrl" :api-base="runtimeConfig.public.apiBase" :inspection="inspection" :primary-content="primaryContent" :load-inspection-file="loadInspectionFile" :revisions="revisions || []" :revisions-error="Boolean(revisionsError)" />
    </main>
  </div>
</template>
