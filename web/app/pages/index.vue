<script setup lang="ts">
import CopyButton from '~/components/CopyButton.vue'
import { RESOURCE_KINDS } from '~/types/marketplace'
import type { ResourceKind, SearchParams } from '~/types/marketplace'
import { safeCanonicalUrl } from '~/lib/presentation'
import { parseSearchQuery, toLocationQuery } from '~/composables/useSearchQuery'
import { useCopyText } from '~/composables/useCopyText'

const route = useRoute()
const router = useRouter()
const api = useMarketplaceApi()
const { copied: copiedMarketplaceCommand, copyError: marketplaceCopyError, copy: copyMarketplaceCommand } = useCopyText()

const { data: metadata, error: metadataError } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: registries } = await useAsyncData('marketplace-registries', () => api.listRegistries())
const searchParams = computed(() => parseSearchQuery(route.query))
const draft = reactive<SearchParams>({ ...searchParams.value })
const filtersOpen = ref(false)
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

const cliMarketplaceDocsUrl = 'https://github.com/Adyen/agent-marketplace/blob/main/docs/adoption-guide.md#install-from-a-marketplace'
const totalPages = computed(() => Math.max(1, Math.ceil((results.value?.total ?? 0) / searchParams.value.limit)))
const currentPage = computed(() => Math.floor(searchParams.value.offset / searchParams.value.limit) + 1)
const hasPreviousPage = computed(() => searchParams.value.offset > 0)
const hasNextPage = computed(() => currentPage.value < totalPages.value)
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.browser_base))
const marketplaceCommand = computed(() => {
  const endpoint = safeCanonicalUrl(metadata.value?.canonical_url)
  return endpoint ? `agent-marketplace install "${endpoint}"` : ''
})
const hasDraftFilters = computed(() => Boolean(draft.q || draft.kind || draft.registry.length))
const searchUpdating = computed(() => {
  const applied = searchParams.value
  return pending.value
    || draft.q !== applied.q
    || draft.kind !== applied.kind
    || draft.limit !== applied.limit
    || draft.registry.length !== applied.registry.length
    || draft.registry.some((registry, index) => registry !== applied.registry[index])
})
const activeFilterCount = computed(() => Number(Boolean(draft.kind)) + draft.registry.length)
const resultCountLabel = computed(() => {
  const total = results.value?.total ?? 0
  return `${total} ${total === 1 ? 'resource' : 'resources'}`
})

const resultStatus = computed(() => {
  if (pending.value) return 'Loading Marketplace resources.'
  if (searchError.value) return 'Search failed. Review the error message and try again.'
  return `${resultCountLabel.value} found.`
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

function resetSearch() {
  clearTimeout(searchTimer)
  searchTimer = undefined
  filtersOpen.value = false
  syncingDraft = true
  Object.assign(draft, parseSearchQuery({}))
  void router.push({ query: {} })
  nextTick(() => { syncingDraft = false })
}

function goToPage(page: number) {
  const offset = (page - 1) * searchParams.value.limit
  router.push({ query: toLocationQuery({ ...searchParams.value, offset }) })
}

function retry() {
  refresh()
}

useHead({
  title: 'Agent Marketplace',
  meta: [
    { name: 'description', content: 'Browse digest-pinned AI agent resources analyzed before publication.' },
  ],
})
</script>

<template>
  <div class="min-h-screen bg-default text-default">
    <MarketplaceSkipLink target="main-content" />

    <header class="border-b border-default">
      <div class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-2 px-4 py-3 sm:px-6">
        <NuxtLink to="/" class="flex items-baseline gap-2 rounded focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">
          <span class="font-mono text-base font-semibold tracking-tight text-highlighted">agent-marketplace</span>
        </NuxtLink>
        <GitHubLink />
      </div>
    </header>

    <main id="main-content" ref="mainContent" tabindex="-1">
      <section class="bg-muted/40">
        <div class="mx-auto max-w-6xl px-4 pb-3 pt-4 sm:px-6 sm:pb-4 sm:pt-5">
          <h1 class="text-2xl font-semibold tracking-tight text-highlighted sm:text-3xl">
            Browse AI resources
          </h1>
          <p class="mt-2 max-w-4xl text-base leading-6 text-muted sm:text-lg sm:leading-7">
            Every revision is pinned and analyzed before publication. Add resources with the
            <a
              :href="cliMarketplaceDocsUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="rounded font-medium text-primary underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
            ><span class="font-mono">agent-marketplace</span> CLI</a>
            and sync them across your AI tools.
          </p>

          <section
            v-if="marketplaceCommand"
            class="mt-2 text-sm leading-6 text-muted"
            aria-labelledby="use-cli-heading"
          >
            <h2 id="use-cli-heading" class="sr-only">Use this Marketplace from the CLI</h2>
            <span>
              Want to discover resources from the CLI?
              <span class="font-medium text-toned">Connect this Marketplace</span>
              <CopyButton
                class="ml-1 align-middle"
                :copied="copiedMarketplaceCommand === 'marketplace'"
                copy-label="Copy Marketplace connection command"
                copied-label="Marketplace connection command copied"
                @click="copyMarketplaceCommand(marketplaceCommand, 'marketplace')"
              />
            </span>
            <span v-if="marketplaceCopyError === 'marketplace'" role="alert" class="text-xs text-error">Clipboard access failed.</span>
          </section>
        </div>
      </section>

      <section class="mx-auto max-w-6xl px-4 sm:px-6" aria-labelledby="results-heading">
        <header class="border-b border-default py-3">
          <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <div class="flex items-baseline gap-2">
              <h2
                id="results-heading"
                tabindex="-1"
                class="rounded text-sm font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary"
              >
                Resources
              </h2>
              <span class="text-xs font-medium text-muted">· {{ results?.total ?? '—' }} total</span>
              <span
                data-testid="search-update-status"
                aria-hidden="true"
                :class="searchUpdating ? 'opacity-100' : 'opacity-0'"
                class="inline-flex items-center gap-1 text-xs text-muted"
              >
                <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin motion-reduce:animate-none" />
                Updating
              </span>
            </div>
          </div>

          <form id="marketplace-search" class="mt-2" role="search" @submit.prevent="submitSearch">
              <div class="grid grid-cols-[minmax(0,1fr)_auto_5.5rem] gap-2 sm:grid-cols-[minmax(0,1fr)_11rem_11rem_5.5rem]">
                <div class="min-w-0">
                  <label for="search-query" class="sr-only">Search</label>
                  <UInput
                    id="search-query"
                    v-model="draft.q"
                    size="lg"
                    placeholder="Search resources"
                    autocomplete="off"
                    class="w-full"
                    :ui="{ base: 'h-9' }"
                  />
                </div>

                <UButton
                  type="button"
                  size="sm"
                  color="neutral"
                  variant="outline"
                  icon="i-lucide-list-filter"
                  aria-controls="resource-filters"
                  :aria-expanded="filtersOpen"
                  class="h-9 min-h-9 self-center justify-center sm:hidden"
                  @click="filtersOpen = !filtersOpen"
                >
                  Filters<span v-if="activeFilterCount"> ({{ activeFilterCount }})</span>
                </UButton>

                <UButton
                  type="button"
                  size="sm"
                  color="primary"
                  variant="link"
                  class="col-start-3 row-start-1 h-9 min-h-9 self-center whitespace-nowrap px-1 text-sm font-medium underline-offset-4 disabled:text-dimmed disabled:opacity-40 disabled:no-underline sm:col-start-4"
                  :class="hasDraftFilters ? 'cursor-pointer' : 'cursor-not-allowed'"
                  :disabled="!hasDraftFilters"
                  @click="resetSearch"
                >
                  Clear filters
                </UButton>

                <div
                  id="resource-filters"
                  :class="[filtersOpen ? 'flex' : 'hidden', 'col-span-3 row-start-2 flex-col gap-2 sm:contents']"
                >
                  <SearchKindFilter
                    id="search-kind"
                    v-model="draft.kind"
                    :kinds="supportedKinds"
                    size="lg"
                    class="h-9 w-full self-center sm:col-start-2 sm:row-start-1"
                    aria-label="Filter by resource kind"
                  />
                  <RegistryFilter
                    id="search-registry"
                    v-model="draft.registry"
                    :items="registryOptions"
                    size="lg"
                    class="h-9 w-full self-center sm:col-start-3 sm:row-start-1"
                    aria-label="Registry"
                  />
                </div>
              </div>
            </form>

            <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ resultStatus }}</p>
        </header>
        <UAlert v-if="metadataError" color="warning" variant="subtle" icon="i-lucide-info" title="Instance metadata is unavailable" description="Search still works, but this instance's capability filters may be incomplete." class="my-4" />

        <div v-if="pending && !results" role="status" aria-live="polite" aria-busy="true" aria-label="Loading resources">
          <div v-for="i in 8" :key="i" class="animate-pulse border-b border-default px-3 py-3 motion-reduce:animate-none sm:px-4">
            <div class="min-w-0 flex-1">
              <div class="h-5 w-1/4 rounded bg-muted" />
              <div class="mt-2 h-5 w-1/3 rounded bg-muted" />
              <div class="mt-2 h-3 w-2/3 rounded bg-muted" />
              <div class="mt-2 h-3 w-1/4 rounded bg-muted" />
            </div>
          </div>
        </div>

        <SearchStatus v-else-if="searchError" state="error" @retry="retry" />
        <SearchStatus v-else-if="!results?.items.length" state="empty" @reset="resetSearch" />

        <template v-else>
          <div
            data-testid="search-results"
            :aria-busy="pending || undefined"
          >
            <div
              data-testid="search-results-body"
              :inert="pending || undefined"
              :class="pending ? 'pointer-events-none opacity-50' : 'opacity-100'"
              class="transition-opacity duration-150 motion-reduce:transition-none"
            >
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
            </div>
          </div>
        </template>
      </section>
    </main>

    <footer class="border-t border-default">
      <div class="mx-auto max-w-6xl px-4 py-5 text-xs text-muted sm:px-6">
        <p>Browse here. Review and install locally with agent-marketplace.</p>
      </div>
    </footer>
  </div>
</template>
