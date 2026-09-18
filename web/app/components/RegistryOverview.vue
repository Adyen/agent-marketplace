<script setup lang="ts">
import { computed } from 'vue'
import { safeCanonicalUrl } from '~/lib/presentation'
import type { Marketplace, Registry } from '~/types/marketplace'

const props = defineProps<{
  registry: Registry
  operator?: Marketplace
  resourceCount?: number
}>()
const operatorCanonicalUrl = computed(() => safeCanonicalUrl(props.operator?.canonical_url))
const publisherUrl = computed(() => safeCanonicalUrl(props.registry.publisher?.url))
const historyPath = computed(() => `/registries/${encodeURIComponent(props.registry.slug)}/history`)
const marketplaceRoute = computed(() => ({ path: '/', query: { registry: props.registry.slug } }))
</script>

<template>
  <article>
    <header>
      <p class="text-sm font-medium text-muted">Registry</p>
      <h1 class="mt-1 break-words text-3xl font-semibold tracking-tight text-highlighted">{{ registry.slug }}</h1>
      <p class="mt-2 max-w-3xl text-base leading-7 text-toned">Resources published through this registry.</p>
      <UButton :to="marketplaceRoute" color="primary" class="mt-4">{{ resourceCount === undefined ? 'View resources' : `View ${resourceCount} resource${resourceCount === 1 ? '' : 's'}` }}</UButton>
    </header>

    <section class="mt-8 border-t border-default pt-6" aria-labelledby="registry-about-heading">
      <h2 id="registry-about-heading" class="text-xl font-semibold text-highlighted">About this registry</h2>
      <dl class="mt-5 grid gap-x-8 gap-y-5 sm:grid-cols-2">
        <div>
          <dt class="text-sm text-muted">Publisher</dt>
          <dd class="mt-1 text-sm text-highlighted">
            <a v-if="publisherUrl" :href="publisherUrl" class="font-medium text-primary underline-offset-4 hover:underline">{{ registry.publisher?.name || publisherUrl }}</a>
            <span v-else class="font-medium">{{ registry.publisher?.name || 'Not provided' }}</span>
          </dd>
        </div>
        <div>
          <dt class="text-sm text-muted">Marketplace status</dt>
          <dd class="mt-1 text-sm text-highlighted">{{ registry.listed ? 'Listed' : 'Unlisted' }}</dd>
        </div>
        <div>
          <dt class="text-sm text-muted">License</dt>
          <dd class="mt-1 text-sm text-highlighted">{{ registry.license || 'Not provided' }}</dd>
        </div>
      </dl>
    </section>

    <section class="mt-8 border-t border-default pt-6" aria-labelledby="registry-technical-heading">
      <h2 id="registry-technical-heading" class="text-xl font-semibold text-highlighted">Technical details</h2>
      <dl class="mt-5 grid gap-x-8 gap-y-5 sm:grid-cols-2">
        <div>
          <dt class="text-sm text-muted">Acquisition</dt>
          <dd class="mt-1 text-sm text-highlighted">{{ registry.acquisition }}</dd>
        </div>
        <div>
          <dt class="text-sm text-muted">Configured ref</dt>
          <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ registry.ref }}</dd>
        </div>
        <div>
          <dt class="text-sm text-muted">Marketplace operator</dt>
          <dd class="mt-1 text-sm text-highlighted">{{ operator?.name || 'Marketplace instance' }}</dd>
          <dd v-if="operatorCanonicalUrl" class="mt-1 break-all text-xs text-muted">{{ operatorCanonicalUrl }}</dd>
        </div>
        <div>
          <dt class="text-sm text-muted">Registry ID</dt>
          <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ registry.id }}</dd>
        </div>
        <div v-if="registry.roots.length" class="sm:col-span-2">
          <dt class="text-sm text-muted">Roots</dt>
          <dd class="mt-1 text-sm text-highlighted">{{ registry.roots.map(root => root.path).join(', ') }}</dd>
        </div>
        <div v-if="registry.origins.length" class="sm:col-span-2">
          <dt class="text-sm text-muted">Origins</dt>
          <dd class="mt-1 break-all text-sm text-highlighted">{{ registry.origins.join(', ') }}</dd>
        </div>
      </dl>
      <NuxtLink :to="historyPath" class="mt-5 inline-block text-sm font-medium text-primary underline-offset-4 hover:underline">View registry history</NuxtLink>
    </section>
  </article>
</template>
