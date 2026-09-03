<script setup lang="ts">
import { RESOURCE_KINDS } from '~/types/marketplace'
import type { ResourceKind, SearchParams } from '~/types/marketplace'
import { apiPath } from '~/lib/api'
import { listAvailabilityCopy, marketplaceInstallCommand, safeCanonicalUrl } from '~/lib/presentation'
import { parseSearchQuery, toLocationQuery } from '~/composables/useSearchQuery'

const route = useRoute()
const router = useRouter()
const api = useMarketplaceApi()
const config = useRuntimeConfig()

const { data: metadata, error: metadataError } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: registries } = await useAsyncData('marketplace-registries', () => api.listRegistries())
const searchParams = computed(() => parseSearchQuery(route.query))
const draft = reactive<SearchParams>({ ...searchParams.value })
let syncingDraft = false
let searchTimer: ReturnType<typeof setTimeout> | undefined

watch(
  () => route.query,
  () => {
    clearTimeout(searchTimer)
    syncingDraft = true
    Object.assign(draft, searchParams.value)
    nextTick(() => { syncingDraft = false })
  },
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
const registryOptions = computed(() => (registries.value ?? []).map(registry => registry.slug))

const limitOptions = [
  { label: '25 per page', value: 25 },
  { label: '50 per page', value: 50 },
  { label: '100 per page', value: 100 },
]

const totalPages = computed(() => Math.max(1, Math.ceil((results.value?.total ?? 0) / searchParams.value.limit)))
const currentPage = computed(() => Math.floor(searchParams.value.offset / searchParams.value.limit) + 1)
const hasPreviousPage = computed(() => searchParams.value.offset > 0)
const hasNextPage = computed(() => currentPage.value < totalPages.value)
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.browser_base))
const marketplaceCommand = computed(() => marketplaceInstallCommand(metadata.value?.canonical_url ?? ''))
const apiEndpoint = computed(() => apiPath(config.public.apiBase, '/search'))
const hasFilters = computed(() => Boolean(searchParams.value.q || searchParams.value.kind || searchParams.value.registry.length))

/*
 * Withheld entries are the norm on this instance, not the exception, so the
 * reason is stated once here rather than repeated on every row. The count is
 * deliberately scoped to "on this page": the search endpoint returns no facet
 * totals, and a number implying it covered all 382 entries would be a lie.
 */
const withheldOnPage = computed(() =>
  (results.value?.items ?? []).filter(item => listAvailabilityCopy(item)?.installable === false).length,
)

const resultStatus = computed(() => {
  if (pending.value) return 'Loading Marketplace resources.'
  if (searchError.value) return 'Search failed. Review the error message and try again.'
  return `${results.value?.total ?? 0} resources available.`
})

watch(() => [draft.q, draft.registry.join('\u0000')], scheduleSearch)
watch(() => [draft.kind, draft.limit], applyDraft)
onBeforeUnmount(() => clearTimeout(searchTimer))

function applyDraft() {
  if (syncingDraft) return
  void router.replace({ query: toLocationQuery({ ...draft, offset: 0 }) })
}

function scheduleSearch() {
  if (syncingDraft) return
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    searchTimer = undefined
    applyDraft()
  }, 250)
}

function submitSearch() {
  clearTimeout(searchTimer)
  searchTimer = undefined
  void router.push({ query: toLocationQuery({ ...draft, offset: 0 }) })
}

function clearSearch() {
  draft.q = ''
  submitSearch()
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
  title: 'Agent resource index',
  meta: [
    { name: 'description', content: 'Search and discover skills, commands, subagents, hooks, and MCP servers.' },
  ],
})
</script>

<template>
  <div class="min-h-screen bg-default text-default">
    <MarketplaceSkipLink target="main-content" />

    <header class="border-b border-default">
      <div class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-2 px-4 py-3 sm:px-6">
        <NuxtLink to="/" class="flex items-baseline gap-2 rounded focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">
          <span class="font-mono text-sm font-medium tracking-tight text-highlighted">agent-marketplace</span>
        </NuxtLink>
        <div class="flex items-center gap-3">
          <GitHubLink />
          <div class="flex items-baseline gap-3 font-mono text-xs text-muted">
            <span v-if="metadata?.instance_id">{{ metadata.instance_id }}</span>
            <span aria-hidden="true">·</span>
            <span>read-only</span>
          </div>
        </div>
      </div>
    </header>

    <main id="main-content" ref="mainContent" tabindex="-1">
      <!-- The index is the page. One line says what this instance is, then the
           controls, then the entries. There is no marketing hero to scroll past. -->
      <section class="border-b border-default bg-muted/40">
        <div class="mx-auto max-w-6xl px-4 py-5 sm:px-6">
          <h1 class="max-w-2xl text-base leading-6 text-toned">
            A verified index of agent resources. Every entry is pinned to a content digest and analyzed before it is offered.
            <span class="text-muted">Installation happens in your CLI.</span>
          </h1>

          <!-- Agent Marketplace speaks to many Marketplaces, so this instance
               says once, in the shape of the command that adopts it, which one
               it is. It sits with the hero rather than between the search
               controls and the entries they filter. The heading deliberately
               avoids the mono-uppercase field-label style used below it: the
               block is a command to take, not a control to fill in. -->
          <section v-if="marketplaceCommand" class="mt-4 max-w-3xl" aria-labelledby="marketplace-install-heading">
            <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <h2 id="marketplace-install-heading" class="text-sm font-semibold text-highlighted">Install this Marketplace</h2>
              <p class="text-xs text-dimmed">Agent Marketplace reads many Marketplaces — this command adds this one.</p>
            </div>

            <CommandBlock :command="marketplaceCommand" label="Marketplace install command" class="mt-2" />
          </section>

          <form class="mt-5 flex flex-col gap-2 sm:flex-row sm:items-end" role="search" @submit.prevent="submitSearch">
            <div class="min-w-0 flex-1">
              <label for="search-query" class="mb-1 block font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Search</label>
              <UInput id="search-query" v-model="draft.q" size="md" icon="i-lucide-search" placeholder="code review, deploy, database" autocomplete="off" :ui="{ trailing: 'pe-1' }" class="w-full">
                <template v-if="draft.q" #trailing>
                  <UButton type="button" color="neutral" variant="link" size="sm" icon="i-lucide-x" aria-label="Clear search" @click="clearSearch" />
                </template>
              </UInput>
            </div>
            <div class="w-full sm:w-44">
              <label for="search-kind" class="mb-1 block font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Kind</label>
              <SearchKindFilter id="search-kind" v-model="draft.kind" :kinds="supportedKinds" size="md" class="w-full" aria-label="Filter by resource kind" />
            </div>
            <div class="w-full sm:w-44">
              <label for="search-registry" class="mb-1 block font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Registry</label>
              <UInputMenu id="search-registry" v-model="draft.registry" multiple :items="registryOptions" size="md" icon="i-lucide-database" trailing-icon="i-lucide-chevron-down" placeholder="any" autocomplete="off" open-on-focus class="w-full" />
            </div>
            <UButton type="submit" size="md" color="primary" class="justify-center">Search</UButton>
          </form>
        </div>
      </section>

      <section class="mx-auto max-w-6xl px-4 sm:px-6" aria-labelledby="results-heading">
        <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-default py-4">
          <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <h2 id="results-heading" tabindex="-1" class="rounded font-mono text-xl font-medium tracking-tight text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">
              {{ results?.total ?? 0 }}
            </h2>
            <span class="text-sm text-muted">{{ hasFilters ? 'matching entries' : 'indexed entries' }}</span>
            <UButton v-if="hasFilters" size="xs" color="neutral" variant="link" class="px-0 underline-offset-4 hover:underline" @click="resetSearch">Clear filters</UButton>
          </div>
          <p v-if="withheldOnPage" class="max-w-md text-xs leading-snug text-muted">
            <span class="font-medium text-warning">{{ withheldOnPage }} of {{ results?.items.length }}</span>
            on this page are not installable. Policy withheld the artifact or an operator revoked the revision; open an entry to read its findings.
          </p>
          <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ resultStatus }}</p>
        </div>

        <UAlert v-if="metadataError" color="warning" variant="subtle" icon="i-lucide-info" title="Instance metadata is unavailable" description="Search still works, but this instance's capability filters may be incomplete." class="my-4" />

        <div v-if="pending" role="status" aria-live="polite" aria-busy="true" aria-label="Loading resources">
          <div v-for="i in 8" :key="i" class="flex animate-pulse gap-3 border-b border-default px-3 py-3 motion-reduce:animate-none sm:px-4">
            <div class="size-[26px] shrink-0 bg-muted" />
            <div class="min-w-0 flex-1">
              <div class="h-4 w-1/3 rounded bg-muted" />
              <div class="mt-2 h-3 w-2/3 rounded bg-muted" />
              <div class="mt-2 h-3 w-1/4 rounded bg-muted" />
            </div>
          </div>
        </div>

        <SearchStatus v-else-if="searchError" state="error" @retry="retry" />
        <SearchStatus v-else-if="!results?.items.length" state="empty" @reset="resetSearch" />

        <template v-else>
          <div>
            <ResourceRow v-for="item in results.items" :key="JSON.stringify([item.registry_id, item.kind, item.name, item.content_digest])" :item="item" :canonical-url="canonicalUrl" />
          </div>

          <nav class="flex flex-col items-center justify-between gap-3 py-5 sm:flex-row" aria-label="Search results pagination">
            <p class="font-mono text-xs text-muted">page {{ currentPage }} / {{ totalPages }}</p>
            <div class="flex items-center gap-2">
              <USelect id="page-size" v-model="draft.limit" :items="limitOptions" value-key="value" size="xs" class="w-32" aria-label="Results per page" @update:model-value="submitSearch" />
              <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-chevron-left" :disabled="!hasPreviousPage" aria-label="Go to previous page" @click="goToPage(currentPage - 1)">Prev</UButton>
              <UButton size="xs" color="neutral" variant="outline" trailing-icon="i-lucide-chevron-right" :disabled="!hasNextPage" aria-label="Go to next page" @click="goToPage(currentPage + 1)">Next</UButton>
            </div>
          </nav>
        </template>
      </section>
    </main>

    <footer class="border-t border-default">
      <div class="mx-auto flex max-w-6xl flex-col gap-1 px-4 py-5 text-xs text-muted sm:flex-row sm:items-center sm:justify-between sm:px-6">
        <p>Read-only discovery. Installation stays in your local CLI.</p>
        <p class="font-mono">searches {{ apiEndpoint }}</p>
      </div>
    </footer>
  </div>
</template>
