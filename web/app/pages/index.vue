<script setup lang="ts">
import { RESOURCE_KINDS } from '~/types/marketplace'
import type { ResourceKind, SearchParams } from '~/types/marketplace'
import { apiPath } from '~/lib/api'
import { kindLabel, safeCanonicalUrl } from '~/lib/presentation'
import { parseSearchQuery, toLocationQuery } from '~/composables/useSearchQuery'

const route = useRoute()
const router = useRouter()
const api = useMarketplaceApi()
const config = useRuntimeConfig()

const { data: metadata, error: metadataError } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const searchParams = computed(() => parseSearchQuery(route.query))
const draft = reactive<SearchParams>({ ...searchParams.value })

watch(
  () => route.query,
  () => Object.assign(draft, searchParams.value),
)

const { data: results, pending, error: searchError, refresh } = await useAsyncData(
  'marketplace-search',
  () => api.search(searchParams.value),
  { watch: [searchParams] },
)

const supportedKinds = computed<ResourceKind[]>(() => {
  const knownMetadata = metadata.value
  if (!knownMetadata) return [...RESOURCE_KINDS]
  return RESOURCE_KINDS.filter(kind => knownMetadata.resource_kinds.includes(kind))
})

const kindOptions = computed(() => [
  { label: 'All resource kinds', value: '' },
  ...supportedKinds.value.map(kind => ({ label: kindLabel(kind), value: kind })),
])

const limitOptions = [
  { label: '12 per page', value: 12 },
  { label: '24 per page', value: 24 },
  { label: '48 per page', value: 48 },
  { label: '100 per page', value: 100 },
]

const totalPages = computed(() => Math.max(1, Math.ceil((results.value?.total ?? 0) / searchParams.value.limit)))
const currentPage = computed(() => Math.floor(searchParams.value.offset / searchParams.value.limit) + 1)
const hasPreviousPage = computed(() => searchParams.value.offset > 0)
const hasNextPage = computed(() => currentPage.value < totalPages.value)
const canonicalUrl = computed(() => {
  const candidate = safeCanonicalUrl(metadata.value?.canonical_url)
  if (candidate) return candidate
  if (import.meta.client) return window.location.origin
  return ''
})
const apiEndpoint = computed(() => apiPath(config.public.apiBase, '/search'))
const resultsHeading = ref<HTMLElement | null>(null)
const resultStatus = computed(() => {
  if (pending.value) return 'Loading Marketplace resources.'
  if (searchError.value) return 'Search failed. Review the error message and try again.'
  return `${results.value?.total ?? 0} resources available.`
})

watch([pending, searchError, results], async () => {
  if (pending.value) return
  await nextTick()
  resultsHeading.value?.focus()
}, { flush: 'post' })

function submitSearch() {
  router.push({ query: toLocationQuery({ ...draft, offset: 0 }) })
}

function resetSearch() {
  router.push({ query: {} })
}

function goToPage(page: number) {
  const offset = (page - 1) * searchParams.value.limit
  router.push({ query: toLocationQuery({ ...searchParams.value, offset }) })
}

function retry() {
  refresh()
}

useHead({
  title: 'Discover agent resources',
  meta: [
    { name: 'description', content: 'Search and discover skills, commands, subagents, hooks, and MCP servers.' },
  ],
})
</script>

<template>
  <div class="min-h-screen bg-muted/30 text-default">
    <a href="#main-content" class="sr-only z-50 rounded-lg bg-primary px-4 py-3 text-white focus:not-sr-only focus:fixed focus:left-4 focus:top-4">Skip to content</a>
    <header class="border-b border-default bg-default/90 backdrop-blur">
      <div class="mx-auto flex max-w-7xl items-center justify-between px-4 py-4 sm:px-6 lg:px-8">
        <NuxtLink to="/" class="flex items-center gap-3 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">
          <span class="flex size-9 items-center justify-center rounded-xl bg-primary text-white" aria-hidden="true">
            <UIcon name="i-lucide-panels-top-left" class="size-5" />
          </span>
          <span class="text-base font-semibold tracking-tight text-highlighted">Agent Marketplace</span>
        </NuxtLink>
        <span class="hidden text-sm text-muted sm:block">Open discovery for agent resources</span>
      </div>
    </header>

    <main id="main-content" tabindex="-1">
      <section class="relative overflow-hidden border-b border-default bg-default">
        <div class="absolute inset-0 bg-[radial-gradient(circle_at_top_right,rgba(20,184,166,0.14),transparent_34rem)]" aria-hidden="true" />
        <div class="relative mx-auto max-w-7xl px-4 pb-12 pt-16 sm:px-6 lg:px-8 lg:pb-16 lg:pt-24">
          <div class="max-w-3xl">
            <p class="mb-4 text-sm font-semibold uppercase tracking-[0.18em] text-primary">Find the right capability</p>
            <h1 class="text-4xl font-semibold tracking-tight text-highlighted sm:text-5xl lg:text-6xl">Discover tools for your next agent workflow.</h1>
            <p class="mt-5 max-w-2xl text-base leading-7 text-muted sm:text-lg">Search approved resources across your Marketplace instance. Browse by kind, narrow by source, and install from the CLI when you are ready.</p>
          </div>

          <form class="mt-10 rounded-2xl border border-default bg-muted/40 p-3 shadow-sm sm:p-4" role="search" @submit.prevent="submitSearch">
            <div class="flex flex-col gap-3 lg:flex-row lg:items-end">
              <div class="min-w-0 flex-1">
                <label for="search-query" class="mb-2 block text-sm font-medium text-highlighted">Search resources</label>
                <UInput id="search-query" v-model="draft.q" size="xl" icon="i-lucide-search" placeholder="Try code review, deploy, or database" autocomplete="off" class="w-full" />
              </div>
              <div class="w-full lg:w-52">
                <label for="search-kind" class="mb-2 block text-sm font-medium text-highlighted">Resource kind</label>
                <USelect id="search-kind" v-model="draft.kind" :items="kindOptions" value-key="value" size="xl" class="w-full" aria-label="Filter by resource kind" />
              </div>
              <div class="w-full lg:w-56">
                <label for="search-source" class="mb-2 block text-sm font-medium text-highlighted">Source</label>
                <UInput id="search-source" v-model="draft.source" size="xl" icon="i-lucide-database" placeholder="Any source" autocomplete="off" class="w-full" />
              </div>
              <UButton type="submit" size="xl" icon="i-lucide-arrow-right" trailing class="justify-center">Search</UButton>
            </div>
            <div class="mt-3 flex flex-wrap items-center justify-between gap-3 px-1">
              <p class="text-xs text-dimmed">Searches <code class="rounded bg-default px-1.5 py-0.5 font-mono">{{ apiEndpoint }}</code></p>
              <div class="flex items-center gap-2">
                <label for="page-size" class="text-xs text-muted">Results per page</label>
                <USelect id="page-size" v-model="draft.limit" :items="limitOptions" value-key="value" size="sm" class="w-36" aria-label="Results per page" />
              </div>
            </div>
          </form>
        </div>
      </section>

      <section class="mx-auto max-w-7xl px-4 py-10 sm:px-6 lg:px-8" aria-labelledby="results-heading">
        <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
          <div>
            <p class="text-sm font-medium text-primary">Marketplace index</p>
            <h2 id="results-heading" ref="resultsHeading" tabindex="-1" class="mt-1 rounded text-2xl font-semibold tracking-tight text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">{{ results?.total ?? 0 }} resources</h2>
            <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ resultStatus }}</p>
          </div>
          <div v-if="searchParams.q || searchParams.kind || searchParams.source" class="flex items-center gap-3">
            <p class="text-sm text-muted">Filtered results</p>
            <UButton size="sm" color="neutral" variant="ghost" @click="resetSearch">Clear filters</UButton>
          </div>
        </div>

        <UAlert v-if="metadataError" color="warning" variant="subtle" icon="i-lucide-info" title="Instance metadata is unavailable" description="Search is still available, but this instance's capability filters may be incomplete." class="mb-6" />

        <div v-if="pending" class="grid gap-5 sm:grid-cols-2 lg:grid-cols-3" aria-label="Loading resources" aria-busy="true">
          <UCard v-for="i in 6" :key="i" class="h-64 animate-pulse motion-reduce:animate-none">
            <div class="h-8 w-2/3 rounded bg-muted" />
            <div class="mt-6 h-4 w-full rounded bg-muted" />
            <div class="mt-3 h-4 w-4/5 rounded bg-muted" />
            <div class="mt-12 h-8 w-1/3 rounded bg-muted" />
          </UCard>
        </div>

        <SearchStatus v-else-if="searchError" state="error" @retry="retry" />
        <SearchStatus v-else-if="!results?.items.length" state="empty" @reset="resetSearch" />

        <template v-else>
          <div class="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
            <ResourceCard v-for="item in results.items" :key="`${item.source_id}:${item.kind}:${item.name}:${item.revision}`" :item="item" :canonical-url="canonicalUrl" />
          </div>

          <nav v-if="totalPages > 1" class="mt-10 flex flex-col items-center justify-between gap-4 border-t border-default pt-6 sm:flex-row" aria-label="Search results pagination">
            <p class="text-sm text-muted">Page {{ currentPage }} of {{ totalPages }}</p>
            <div class="flex items-center gap-2">
              <UButton color="neutral" variant="outline" icon="i-lucide-chevron-left" :disabled="!hasPreviousPage" aria-label="Go to previous page" @click="goToPage(currentPage - 1)">Previous</UButton>
              <UButton color="neutral" variant="outline" trailing-icon="i-lucide-chevron-right" :disabled="!hasNextPage" aria-label="Go to next page" @click="goToPage(currentPage + 1)">Next</UButton>
            </div>
          </nav>
        </template>
      </section>
    </main>

    <footer class="border-t border-default bg-default">
      <div class="mx-auto flex max-w-7xl flex-col gap-2 px-4 py-8 text-sm text-muted sm:flex-row sm:items-center sm:justify-between sm:px-6 lg:px-8">
        <p>Read-only discovery. Installation stays in your local CLI.</p>
        <p v-if="metadata?.instance_id" class="font-mono text-xs">{{ metadata.instance_id }}</p>
      </div>
    </footer>
  </div>
</template>
