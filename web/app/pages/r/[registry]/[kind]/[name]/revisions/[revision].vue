<script setup lang="ts">
import { computed } from 'vue'
import { isResourceKind } from '~/lib/api'
import { safeCanonicalUrl } from '~/lib/presentation'
import { resourceAsyncDataKey, resourceInspectionAsyncDataKey, resourceInspectionContentAsyncDataKey, revisionAsyncDataKey, routeParam } from '~/lib/routes'
import { isSha256Digest } from '~/lib/validation'
import type { InspectionFile } from '~/types/marketplace'

const route = useRoute()
const api = useMarketplaceApi()
const runtimeConfig = useRuntimeConfig()

const registry = routeParam(route.params.registry, 'registry')
const kindValue = routeParam(route.params.kind, 'resource kind')
const name = routeParam(route.params.name, 'resource name')
const revision = routeParam(route.params.revision, 'revision')
if (!isResourceKind(kindValue)) throw createError({ statusCode: 404, statusMessage: 'Resource kind not found' })
if (!isSha256Digest(revision)) throw createError({ statusCode: 404, statusMessage: 'Resource revision not found' })
const kind = kindValue

const { data: metadata } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: registryRecord } = await useAsyncData(`marketplace-registry:${registry}`, () => api.getRegistry(registry))
const { data: resource, pending, error, refresh } = await useAsyncData(
  revisionAsyncDataKey(registry, kind, name, revision),
  () => api.getResourceRevision(registry, kind, name, revision),
)
const { data: revisions, error: revisionsError } = await useAsyncData(
  `${resourceAsyncDataKey(registry, kind, name)}:revisions`,
  () => api.listResourceRevisions(registry, kind, name),
)
const { data: inspection } = await useAsyncData(
  resourceInspectionAsyncDataKey(registry, kind, name, revision),
  () => api.getResourceInspection(registry, kind, name, revision),
  { immediate: true },
)
const { data: primaryContent } = await useAsyncData<string | undefined>(
  resourceInspectionContentAsyncDataKey(registry, kind, name, revision),
  () => {
    const primary = inspection.value?.files.find(file => file.path === inspection.value?.primary_path)
    if (!primary || primary.inspection_class === 'image' || primary.inspection_class === 'metadata') return Promise.resolve(undefined)
    return api.getResourceInspectionContent(registry, kind, name, revision, primary.media_type) as Promise<string>
  },
  { immediate: true },
)
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.browser_base))

function loadInspectionFile(file: InspectionFile): Promise<string | Blob> {
  return api.getResourceInspectionFile(registry, kind, name, revision, file.content_digest, file.media_type)
}

function retry() {
  refresh()
}

useHead(() => ({
  title: resource.value ? `${name} · Agent Marketplace` : 'Resource revision · Agent Marketplace',
  meta: [{ name: 'description', content: 'Read-only resource provenance, security, and acquisition details.' }],
}))
</script>

<template>
  <div class="min-h-screen bg-muted/30 text-default">
    <MarketplaceSkipLink target="resource-content" />
    <MarketplaceRecordHeader :breadcrumbs="[{ label: 'Resource revision' }]" />

    <main id="resource-content" class="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8" tabindex="-1">
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-default p-8">
        <h1 class="text-2xl font-semibold text-highlighted">Loading resource revision</h1>
        <p class="mt-3 text-sm text-muted">Fetching this exact verified revision and its metadata.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-default p-8">
        <h1 class="text-2xl font-semibold text-error">Resource revision unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-muted">This exact revision could not be loaded. It may be revoked, retained only by policy, or unavailable from the Marketplace.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <ResourceDetailView v-else-if="resource" :resource="resource" :registry="registry" :acquisition="registryRecord?.acquisition" :kind="kind" :name="name" :canonical-url="canonicalUrl" :api-base="runtimeConfig.public.apiBase" :inspection="inspection" :primary-content="primaryContent" :load-inspection-file="loadInspectionFile" :revisions="revisions || []" :revisions-error="Boolean(revisionsError)" />
    </main>
  </div>
</template>
