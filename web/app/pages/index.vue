<script setup lang="ts">
import { RESOURCE_KINDS } from '~/types/marketplace'
import type { ResourceKind, ResourceListItem, SearchParams } from '~/types/marketplace'
import { kindLabel, safeCanonicalUrl } from '~/lib/presentation'
import { DOCS_PATH } from '~/lib/site'
import { parseSearchQuery, toLocationQuery } from '~/composables/useSearchQuery'

const route = useRoute()
const router = useRouter()
const api = useMarketplaceApi()

const { data: metadata, error: metadataError } = await useAsyncData('marketplace-metadata', () => api.getWellKnown())
const { data: registries } = await useAsyncData('marketplace-registries', () => api.listRegistries())
const searchParams = computed(() => parseSearchQuery(route.query))
const draft = reactive<SearchParams>({ ...searchParams.value })
const filtersOpen = ref(false)
const additionalItems = ref<ResourceListItem[]>([])
const loadingMore = ref(false)
const loadMoreError = ref<unknown>(null)
const loadMoreTrigger = ref<HTMLElement | null>(null)
let syncingDraft = false
let searchGeneration = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined

watch(
  () => route.query,
  () => {
    clearTimeout(searchTimer)
    searchGeneration++
    additionalItems.value = []
    loadingMore.value = false
    loadMoreError.value = null
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

const displayedItems = computed(() => [...(results.value?.items ?? []), ...additionalItems.value])
const hasMoreResults = computed(() => displayedItems.value.length < (results.value?.total ?? 0))
const canonicalUrl = computed(() => safeCanonicalUrl(metadata.value?.canonical_url))
const activeFilterCount = computed(() => Number(Boolean(draft.kind)) + draft.registry.length)
const resultCountLabel = computed(() => {
  const total = results.value?.total ?? 0
  return `${total} ${total === 1 ? 'resource' : 'resources'}`
})
const searchUpdating = computed(() => {
  const applied = searchParams.value
  return pending.value
    || draft.q !== applied.q
    || draft.kind !== applied.kind
    || draft.registry.length !== applied.registry.length
    || draft.registry.some((registry, index) => registry !== applied.registry[index])
})
const resultStatus = computed(() => {
  if (pending.value) return 'Loading Marketplace resources.'
  if (searchError.value) return 'Search failed. Review the error message and try again.'
  if (loadingMore.value) return `Loading more Marketplace resources. ${displayedItems.value.length} currently shown.`
  if (loadMoreError.value) return `Additional resources could not be loaded. ${displayedItems.value.length} remain available.`
  return `${resultCountLabel.value} found. ${displayedItems.value.length} shown.`
})

watch(() => [draft.q, draft.registry.join('\u0000')], scheduleSearch)
watch(() => draft.kind, applyDraft)
watch(loadMoreTrigger, (element, _previous, onCleanup) => {
  if (!import.meta.client || !element || !('IntersectionObserver' in window)) return
  const observer = new IntersectionObserver((entries) => {
    if (entries.some(entry => entry.isIntersecting) && !loadMoreError.value) void loadMore()
  }, { rootMargin: '300px 0px' })
  observer.observe(element)
  onCleanup(() => observer.disconnect())
})
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

function updateKind(kind: ResourceKind | '') {
  draft.kind = kind
}

function updateRegistries(registry: string[]) {
  draft.registry = registry
}

function removeRegistry(registry: string) {
  draft.registry = draft.registry.filter(value => value !== registry)
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

async function loadMore() {
  const currentResults = results.value
  if (pending.value || loadingMore.value || !currentResults || !hasMoreResults.value) return
  const generation = searchGeneration
  const offset = currentResults.offset + displayedItems.value.length
  loadingMore.value = true
  loadMoreError.value = null
  try {
    const page = await api.search({ ...searchParams.value, offset })
    if (generation !== searchGeneration) return
    if (page.offset !== offset || (!page.items.length && offset < page.total)) throw new Error('Marketplace search returned a non-progressing page')
    additionalItems.value.push(...page.items)
    currentResults.total = page.total
  } catch (error) {
    if (generation === searchGeneration) loadMoreError.value = error
  } finally {
    if (generation === searchGeneration) loadingMore.value = false
  }
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
  <div class="marketplace-home">
    <main id="main-content" class="marketplace-main" tabindex="-1">
      <section class="marketplace-intro">
        <div class="marketplace-intro__inner">
          <h1>{{ metadata?.instance_id ?? 'Agent Marketplace' }}</h1>
          <div class="marketplace-intro__description">
            <p>Browse resources for AI agents, with every revision pinned and analyzed before publication.</p>
            <p>
              To use a resource, install the
              <a href="/">agent-marketplace</a>
              CLI, then copy and run its install command.
            </p>
          </div>
        </div>
      </section>

      <section
        data-testid="resources-section"
        class="marketplace-catalog"
        aria-labelledby="results-heading"
      >
        <div class="marketplace-catalog__inner">
          <div class="marketplace-catalog__layout">
            <aside class="catalog-sidebar" aria-labelledby="filters-heading">
              <div class="catalog-sidebar__sticky">
                <h2 id="filters-heading">Filters</h2>
                <CatalogFilterRail
                  :kind="draft.kind"
                  :registry="draft.registry"
                  :registries="registries ?? []"
                  :kinds="supportedKinds"
                  @update:kind="updateKind"
                  @update:registry="updateRegistries"
                />
              </div>
            </aside>

            <div class="catalog-results">
              <form
                id="marketplace-search"
                class="catalog-search"
                role="search"
                @submit.prevent="submitSearch"
              >
                <label for="search-query" class="sr-only">Search</label>
                <UInput
                  id="search-query"
                  v-model="draft.q"
                  size="lg"
                  placeholder="Search resources..."
                  autocomplete="off"
                  icon="i-lucide-search"
                  color="primary"
                  class="w-full"
                  :ui="{ base: 'h-10 focus-visible:ring-primary' }"
                />
                <UButton
                  type="button"
                  size="sm"
                  color="neutral"
                  variant="outline"
                  icon="i-lucide-list-filter"
                  aria-controls="mobile-resource-filters"
                  :aria-expanded="filtersOpen"
                  class="mobile-filter-toggle"
                  @click="filtersOpen = !filtersOpen"
                >
                  Filters<span v-if="activeFilterCount"> ({{ activeFilterCount }})</span>
                </UButton>
              </form>

              <div
                id="mobile-resource-filters"
                class="mobile-resource-filters"
                :data-open="filtersOpen"
              >
                <CatalogFilterRail
                  id-prefix="mobile-catalog-filter"
                  :kind="draft.kind"
                  :registry="draft.registry"
                  :registries="registries ?? []"
                  :kinds="supportedKinds"
                  @update:kind="updateKind"
                  @update:registry="updateRegistries"
                />
              </div>

              <header class="catalog-results__header">
                <h2 id="results-heading" tabindex="-1">Resources</h2>
                <p>
                  {{ resultCountLabel }}
                  <span
                    data-testid="search-update-status"
                    :class="{ 'catalog-results__updating--visible': searchUpdating }"
                    class="catalog-results__updating"
                  >
                    · Updating…
                  </span>
                </p>
              </header>

              <div
                v-if="activeFilterCount"
                class="catalog-active-filters"
                aria-label="Active filters"
              >
                <button
                  v-if="draft.kind"
                  type="button"
                  class="catalog-filter-chip"
                  :aria-label="`Remove ${kindLabel(draft.kind)} kind filter`"
                  @click="draft.kind = ''"
                >
                  {{ kindLabel(draft.kind) }}
                  <UIcon name="i-lucide-x" aria-hidden="true" />
                </button>
                <button
                  v-for="registry in draft.registry"
                  :key="registry"
                  type="button"
                  class="catalog-filter-chip"
                  :aria-label="`Remove ${registry} registry filter`"
                  @click="removeRegistry(registry)"
                >
                  {{ registry }}
                  <UIcon name="i-lucide-x" aria-hidden="true" />
                </button>
                <UButton
                  type="button"
                  size="sm"
                  color="primary"
                  variant="link"
                  class="clear-filters"
                  @click="resetSearch"
                >
                  Clear
                </UButton>
              </div>

              <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">
                {{ resultStatus }}
              </p>

              <UAlert
                v-if="metadataError"
                color="warning"
                variant="subtle"
                icon="i-lucide-info"
                title="Instance metadata is unavailable"
                description="Search still works, but this instance's capability filters may be incomplete."
                class="my-4"
              />

              <div
                v-if="pending && !results"
                role="status"
                aria-live="polite"
                aria-busy="true"
                aria-label="Loading resources"
                class="catalog-search-content"
              >
                <div
                  v-for="i in 8"
                  :key="i"
                  class="animate-pulse border-b border-default px-3 py-3 motion-reduce:animate-none sm:px-4"
                >
                  <div class="min-w-0 flex-1">
                    <div class="h-5 w-1/4 rounded bg-muted" />
                    <div class="mt-2 h-5 w-1/3 rounded bg-muted" />
                    <div class="mt-2 h-3 w-2/3 rounded bg-muted" />
                    <div class="mt-2 h-3 w-1/4 rounded bg-muted" />
                  </div>
                </div>
              </div>

              <SearchStatus v-else-if="searchError" class="catalog-search-content" state="error" @retry="retry" />
              <SearchStatus v-else-if="!results?.items.length" class="catalog-search-content" state="empty" @reset="resetSearch" />

              <div
                v-else
                data-testid="search-results"
                class="catalog-search-content"
                :aria-busy="pending || loadingMore || undefined"
              >
                <div
                  data-testid="search-results-body"
                  :inert="pending || undefined"
                  :class="pending ? 'pointer-events-none' : ''"
                >
                  <div>
                    <ResourceRow
                      v-for="item in displayedItems"
                      :key="JSON.stringify([item.registry_id, item.kind, item.name, item.content_digest])"
                      :item="item"
                      :canonical-url="canonicalUrl"
                    />
                  </div>

                  <div
                    v-if="hasMoreResults"
                    ref="loadMoreTrigger"
                    data-testid="load-more-trigger"
                    class="catalog-stream-status"
                  >
                    <p v-if="loadMoreError" role="alert">Additional resources could not be loaded.</p>
                    <UButton
                      size="sm"
                      color="neutral"
                      variant="outline"
                      :loading="loadingMore"
                      :disabled="loadingMore"
                      :aria-label="loadMoreError ? 'Retry loading resources' : 'Load more resources'"
                      @click="loadMore"
                    >
                      {{ loadMoreError ? 'Try again' : loadingMore ? 'Loading more…' : 'Load more' }}
                    </UButton>
                  </div>
                  <p v-else class="catalog-stream-complete">All {{ displayedItems.length }} resources loaded.</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </main>

    <footer class="marketplace-footer">
      <div>
        <a :href="DOCS_PATH">Docs</a>
        <p><span v-if="metadata">Version {{ metadata.version }} · </span>built at <span class="marketplace-footer__adyen">Adyen</span></p>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.marketplace-home {
  display: flex;
  min-height: calc(100vh - 4rem);
  flex-direction: column;
  background: var(--am-color-bg-canvas);
  color: var(--am-color-text-primary);
}

.marketplace-main {
  display: flex;
  flex: 1;
  flex-direction: column;
}

.marketplace-intro {
  border-bottom: 1px solid var(--am-color-border-default);
  background: var(--am-color-bg-canvas);
}

.marketplace-intro__inner {
  width: 100%;
  max-width: 72rem;
  margin: 0 auto;
  padding: 1rem;
}

.marketplace-intro h1 {
  font-size: clamp(1.85rem, 4vw, 2.4rem);
  font-weight: 750;
  letter-spacing: -0.035em;
  line-height: 1.12;
}

.marketplace-intro__description {
  margin-top: 0.5rem;
  color: var(--am-color-text-secondary);
  font-size: 1rem;
  line-height: 1.5;
}

.marketplace-intro__description p {
  display: inline;
}

.marketplace-intro__description p + p {
  margin-left: 0.25rem;
}

.marketplace-intro__description a {
  color: var(--am-color-action-primary);
  font-weight: 500;
  text-decoration: underline;
  text-underline-offset: 4px;
  white-space: nowrap;
}

.marketplace-catalog {
  flex: 1;
  background: var(--am-color-bg-surface);
}

.marketplace-catalog__inner {
  width: 100%;
  max-width: 72rem;
  margin: 0 auto;
  padding: 0 1rem;
}

.marketplace-catalog__layout,
.catalog-results {
  min-width: 0;
}

.catalog-sidebar {
  display: none;
}

.catalog-results {
  padding-top: 1.25rem;
}

.catalog-search {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.65rem;
}

.mobile-filter-toggle {
  min-height: 2.5rem;
  cursor: pointer;
}

.mobile-resource-filters {
  display: none;
  margin-top: 0.8rem;
  padding: 1rem;
  border: 1px solid var(--am-color-border-default);
  border-radius: 0.55rem;
  background: var(--am-color-bg-canvas);
}

.mobile-resource-filters[data-open="true"] {
  display: block;
}

.catalog-results__header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-top: 1rem;
}

.catalog-results__header h2 {
  width: fit-content;
  border-radius: 0.2rem;
  font-size: 1.35rem;
  font-weight: 750;
  letter-spacing: -0.025em;
}

.catalog-results__header h2:focus-visible {
  outline: 2px solid var(--am-color-action-focus-ring);
  outline-offset: 4px;
}

.catalog-results__header p {
  margin: 0;
  color: var(--am-color-text-muted);
  font-size: 0.8rem;
  font-weight: 550;
}

.catalog-results__updating {
  opacity: 0;
}

.catalog-results__updating--visible {
  opacity: 1;
}

.catalog-active-filters {
  display: flex;
  min-height: 30px;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
}

.catalog-filter-chip {
  display: inline-flex;
  min-height: 30px;
  align-items: center;
  gap: 5px;
  padding: 3px 8px 3px 10px;
  border: 1px solid var(--am-color-border-default);
  border-radius: 999px;
  background: var(--am-color-bg-subtle);
  color: var(--am-color-text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
}

.catalog-filter-chip:hover {
  border-color: var(--am-color-border-strong);
  background: var(--am-color-bg-hover);
  color: var(--am-color-text-primary);
}

.catalog-filter-chip:focus-visible {
  outline: 2px solid var(--am-color-action-focus-ring);
  outline-offset: 2px;
}

.catalog-filter-chip :deep(svg) {
  width: 0.8rem;
  height: 0.8rem;
}

.clear-filters {
  min-height: 30px;
  padding: 3px 6px;
  cursor: pointer;
}

.catalog-search-content {
  margin-top: 14px;
}

.catalog-stream-status {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.75rem;
  padding: 1.25rem 0;
}

.catalog-stream-status p,
.catalog-stream-complete {
  padding: 1.25rem 0;
  color: var(--am-color-text-muted);
  font-size: 0.8rem;
  text-align: center;
}

.catalog-stream-status p {
  padding: 0;
}

.marketplace-footer {
  border-top: 1px solid var(--am-color-border-default);
  background: var(--am-color-bg-canvas);
}

.marketplace-footer > div {
  display: flex;
  width: 100%;
  max-width: 72rem;
  flex-direction: column;
  gap: 0.5rem;
  margin: 0 auto;
  padding: 1.25rem 1rem;
  color: var(--am-color-text-muted);
  font-family: var(--font-sans);
  font-size: var(--am-text-body-sm);
  font-weight: var(--am-font-medium);
}

.marketplace-footer__adyen {
  color: var(--am-color-action-primary);
  font-weight: var(--am-font-semibold);
}

.marketplace-footer a {
  text-decoration: underline;
  text-decoration-color: var(--am-color-border-strong);
  text-underline-offset: 4px;
}

.marketplace-footer a:hover {
  color: var(--am-color-text-primary);
}

@media (min-width: 40rem) {
  .marketplace-intro__inner,
  .marketplace-catalog__inner {
    padding-right: 1.5rem;
    padding-left: 1.5rem;
  }

  .marketplace-footer > div {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    padding-right: 1.5rem;
    padding-left: 1.5rem;
  }
}

@media (min-width: 48rem) {
  .marketplace-catalog__layout {
    display: grid;
    grid-template-columns: 13.5rem minmax(0, 1fr);
    gap: 1.5rem;
  }

  .catalog-sidebar {
    display: block;
    min-width: 0;
    padding: 1.55rem 1rem 2rem 0;
    border-right: 1px solid var(--am-color-border-default);
  }

  .catalog-sidebar__sticky {
    position: sticky;
    top: 88px;
    max-height: max(0px, calc(100vh - 240px));
    overflow-y: auto;
  }

  .catalog-sidebar h2 {
    margin-bottom: 1.3rem;
    font-size: 0.8rem;
    font-weight: 800;
    letter-spacing: 0.1em;
    text-transform: uppercase;
  }

  .catalog-results {
    padding-top: 1.55rem;
  }

  .catalog-search {
    display: block;
  }

  .mobile-filter-toggle,
  .mobile-resource-filters {
    display: none !important;
  }
}

@media (min-width: 64rem) {
  .marketplace-catalog__layout {
    grid-template-columns: 15rem minmax(0, 1fr);
    gap: 1.5rem;
  }
}
</style>
