<script setup lang="ts">
import { computed } from 'vue'
import { isResourceKind } from '~/lib/api'
import { safeCanonicalUrl } from '~/lib/presentation'
import { resourceAsyncDataKey, resourceInspectionAsyncDataKey, resourceInspectionContentAsyncDataKey, routeParam } from '~/lib/routes'
import { isSha256Digest } from '~/lib/validation'
import type { InspectionFile } from '~/types/marketplace'

const route = useRoute()
const api = useMarketplaceApi()
const runtimeConfig = useRuntimeConfig()

const registry = routeParam(route.params.registry, 'registry')
const kindValue = routeParam(route.params.kind, 'resource kind')
const name = routeParam(route.params.name, 'resource name')
if (!isResourceKind(kindValue)) throw createError({ statusCode: 404, statusMessage: 'Resource kind not found' })
const kind = kindValue

const { data: metadata } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: registryRecord } = await useAsyncData(`marketplace-registry:${registry}`, () => api.getRegistry(registry))
const { data: operator } = await useAsyncData('marketplace-instance', () => api.getInstance())
const { data: resource, pending, error, refresh } = await useAsyncData(
  resourceAsyncDataKey(registry, kind, name),
  () => api.getResource(registry, kind, name),
)
const { data: revisions, error: revisionsError } = await useAsyncData(
  `${resourceAsyncDataKey(registry, kind, name)}:revisions`,
  () => api.listResourceRevisions(registry, kind, name),
)
const { data: relatedResources } = await useAsyncData(
  `${resourceAsyncDataKey(registry, kind, name)}:related`,
  () => kind === 'collection' ? api.search({ q: '', kind: '', registry: [registry], offset: 0, limit: 100 }) : Promise.resolve(null),
)
const revision = computed(() => {
  const digest = resource.value?.content_digest ?? ''
  return isSha256Digest(digest) ? digest : ''
})
const inspectionKey = computed(() => resourceInspectionAsyncDataKey(registry, kind, name, revision.value || 'pending'))
const inspectionEnabled = computed(() => Boolean(revision.value))
const { data: inspection } = await useAsyncData(
  inspectionKey,
  () => api.getResourceInspection(registry, kind, name, revision.value),
  { immediate: true, enabled: inspectionEnabled },
)
const inspectionReady = computed(() => Boolean(revision.value && inspection.value?.revision_digest === revision.value))
const primaryContentKey = computed(() => resourceInspectionContentAsyncDataKey(registry, kind, name, revision.value || 'pending'))
const { data: primaryContent } = await useAsyncData<string | undefined>(
  primaryContentKey,
  () => {
    if (!inspectionReady.value) return Promise.resolve(undefined)
    const primary = inspection.value?.files.find(file => file.path === inspection.value?.primary_path)
    if (!primary || primary.inspection_class === 'image' || primary.inspection_class === 'metadata') return Promise.resolve(undefined)
    return api.getResourceInspectionContent(registry, kind, name, revision.value, primary.media_type) as Promise<string>
  },
  { immediate: true, enabled: inspectionReady, watch: [inspection] },
)
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.canonical_url))
const browserBase = computed(() => safeCanonicalUrl(metadata.value?.browser_base))

function loadInspectionFile(file: InspectionFile): Promise<string | Blob> {
  return api.getResourceInspectionFile(registry, kind, name, revision.value, file.content_digest, file.media_type)
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
  <div class="min-h-screen bg-[var(--am-color-bg-canvas)] text-default">
    <main id="main-content" class="mx-auto max-w-6xl px-4 py-6 sm:px-6 sm:py-8 lg:px-8" tabindex="-1">
      <MarketplaceRecordHeader :breadcrumbs="[{ label: 'Marketplace', to: '/' }, { label: `${registry}/${name}` }]" />
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-[var(--am-color-bg-surface)] p-8">
        <h1 class="text-2xl font-semibold text-highlighted">Loading resource</h1>
        <p class="mt-3 text-sm text-toned">Fetching the latest verified revision and its metadata.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-[var(--am-color-bg-surface)] p-8">
        <h1 class="text-2xl font-semibold text-error">Resource unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-toned">This resource could not be loaded. The Marketplace may be unavailable or the resource may no longer be listed.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <ResourceDetailView v-else-if="resource" :resource="resource" :registry="registry" :acquisition="registryRecord?.acquisition" :registry-details="registryRecord" :operator="operator" :kind="kind" :name="name" :canonical-url="canonicalUrl" :browser-base="browserBase" :api-base="runtimeConfig.public.apiBase" :inspection="inspection" :primary-content="primaryContent" :load-inspection-file="loadInspectionFile" :revisions="revisions || []" :revisions-error="Boolean(revisionsError)" :current-revision-digest="resource.content_digest" :related-resources="relatedResources?.items || []" :initial-tab="route.query.tab === 'revisions' ? 'revisions' : 'content'" />
    </main>
  </div>
</template>
