<script setup lang="ts">
import { isResourceKind } from '~/lib/api'
import { resourceAuditAsyncDataKey, routeParam } from '~/lib/routes'
import { isSha256Digest } from '~/lib/validation'

const route = useRoute()
const api = useMarketplaceApi()

const registry = routeParam(route.params.registry, 'registry')
const kindValue = routeParam(route.params.kind, 'resource kind')
const name = routeParam(route.params.name, 'resource name')
const revision = routeParam(route.params.revision, 'revision')
const analyzerId = routeParam(route.params.analyzerId, 'analyzer id')
if (!isResourceKind(kindValue)) throw createError({ statusCode: 404, statusMessage: 'Resource kind not found' })
if (!isSha256Digest(revision)) throw createError({ statusCode: 404, statusMessage: 'Resource revision not found' })
const kind = kindValue

const { data: audit, pending, error, refresh } = await useAsyncData(
  resourceAuditAsyncDataKey(registry, kind, name, revision, analyzerId),
  () => api.getResourceAudit(registry, kind, name, revision, analyzerId),
)

function retry() {
  refresh()
}

useHead(() => ({
  title: audit.value ? `${audit.value.analyzer_id} · Audit · Agent Marketplace` : 'Analyzer audit · Agent Marketplace',
  meta: [{ name: 'description', content: 'Read-only analyzer audit detail and findings.' }],
}))
</script>

<template>
  <div class="min-h-screen bg-muted/30 text-default">
    <MarketplaceRecordHeader :breadcrumbs="[{ label: 'Analyzer audit' }]" />

    <main id="main-content" class="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8" tabindex="-1">
      <div v-if="pending" role="status" aria-live="polite" aria-busy="true" class="rounded-2xl border border-default bg-default p-8">
        <h1 class="text-2xl font-semibold text-highlighted">Loading analyzer audit</h1>
        <p class="mt-3 text-sm text-muted">Fetching this immutable analyzer audit and its findings.</p>
      </div>
      <section v-else-if="error" role="alert" class="rounded-2xl border border-error/30 bg-default p-8">
        <h1 class="text-2xl font-semibold text-error">Analyzer audit unavailable</h1>
        <p class="mt-3 text-sm leading-6 text-muted">This audit could not be loaded. The analyzer may not have run for this revision, or the Marketplace may be unavailable.</p>
        <UButton class="mt-6" color="error" @click="retry">Try again</UButton>
      </section>
      <ResourceAuditDetailView v-else-if="audit" :audit="audit" :registry="registry" :kind="kind" :name="name" :revision="revision" />
    </main>
  </div>
</template>
