<script setup lang="ts">
import { computed } from 'vue'
import { isResourceKind } from '~/lib/api'
import { safeCanonicalUrl } from '~/lib/presentation'
import { revisionAsyncDataKey, routeParam } from '~/lib/routes'
import { isSha256Digest } from '~/lib/validation'

const route = useRoute()
const api = useMarketplaceApi()

const source = routeParam(route.params.source, 'source')
const kindValue = routeParam(route.params.kind, 'resource kind')
const name = routeParam(route.params.name, 'resource name')
const revision = routeParam(route.params.revision, 'revision')
if (!isResourceKind(kindValue)) throw createError({ statusCode: 404, statusMessage: 'Resource kind not found' })
if (!isSha256Digest(revision)) throw createError({ statusCode: 404, statusMessage: 'Resource revision not found' })
const kind = kindValue

const { data: metadata } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: resource, pending, error, refresh } = await useAsyncData(
  revisionAsyncDataKey(source, kind, name, revision),
  () => api.getResourceRevision(source, kind, name, revision),
)
const inspectionKey = `${revisionAsyncDataKey(source, kind, name, revision)}:inspection`
const { data: inspection } = await useAsyncData(
  inspectionKey,
  () => api.getResourceInspection(source, kind, name, revision),
  { immediate: kind === 'skill' },
)
const { data: primaryContent } = await useAsyncData(
  `${inspectionKey}:content`,
  () => api.getResourceInspectionContent(source, kind, name, revision),
  { immediate: kind === 'skill' },
)
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.browser_base))

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
    <a href="#resource-content" class="sr-only z-50 rounded-lg bg-primary px-4 py-3 text-white focus:not-sr-only focus:fixed focus:left-4 focus:top-4">Skip to content</a>
    <header class="border-b border-default bg-default/90">
      <div class="mx-auto flex max-w-5xl items-center px-4 py-4 sm:px-6 lg:px-8">
        <NuxtLink to="/" class="rounded-lg text-sm font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Agent Marketplace</NuxtLink>
        <span class="mx-3 text-muted" aria-hidden="true">/</span>
        <span class="truncate text-sm text-muted">Resource revision</span>
      </div>
    </header>

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
      <ResourceDetailView v-else-if="resource" :resource="resource" :source="source" :kind="kind" :name="name" :canonical-url="canonicalUrl" :inspection="inspection" :primary-content="primaryContent" />
    </main>
  </div>
</template>
