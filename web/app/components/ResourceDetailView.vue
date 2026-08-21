<script setup lang="ts">
import { computed } from 'vue'
import { installCommand, kindIcon, kindLabel, resourceLocator, sourceLabel } from '~/lib/presentation'
import { useCopyText } from '~/composables/useCopyText'
import type { ResourceKind, ResourceListItem, ResourceRevision } from '~/types/marketplace'

type Props = {
  resource: ResourceRevision
  source: string
  kind: ResourceKind
  name: string
  canonicalUrl: string
}

const props = defineProps<Props>()
const { copied, copyError, copy } = useCopyText()

const listItem = computed<ResourceListItem>(() => ({
  source_id: props.source,
  kind: props.kind,
  name: props.name,
  revision: props.resource.content_digest,
  content_digest: props.resource.content_digest,
}))
const browserUrl = computed(() => resourceLocator(listItem.value, props.canonicalUrl))
const command = computed(() => installCommand(listItem.value, props.canonicalUrl))
const actionsAvailable = computed(() => Boolean(browserUrl.value && command.value))
const revoked = computed(() => Boolean(props.resource.revocation && Object.keys(props.resource.revocation).length))
const acquisition = computed(() => props.resource.acquisition || valueFromRecord(props.resource.provenance, 'acquisition') || 'Not specified')
const provenanceEntries = computed(() => Object.entries(props.resource.provenance ?? {}))

function valueFromRecord(record: Record<string, unknown> | undefined, key: string): string {
  const value = record?.[key]
  return value === undefined ? '' : formatValue(value)
}

function formatValue(value: unknown): string {
  if (typeof value === 'string') return value
  if (value === undefined) return 'Not provided'
  try {
    return JSON.stringify(value)
  } catch {
    return 'Unavailable'
  }
}
</script>

<template>
  <article class="rounded-2xl border border-default bg-default p-6 shadow-sm sm:p-8">
    <div class="flex flex-wrap items-start justify-between gap-4 border-b border-default pb-6">
      <div class="flex min-w-0 items-start gap-4">
        <div class="flex size-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary" aria-hidden="true">
          <UIcon :name="kindIcon(kind)" class="size-6" />
        </div>
        <div class="min-w-0">
          <p class="text-sm font-medium text-primary">{{ sourceLabel(listItem) }}</p>
          <h1 class="mt-1 break-words text-3xl font-semibold tracking-tight text-highlighted">{{ name }}</h1>
          <p class="mt-2 text-sm text-muted">{{ kindLabel(kind) }} resource</p>
        </div>
      </div>
      <UBadge :color="revoked ? 'error' : 'success'" variant="subtle">{{ revoked ? 'Revoked' : 'Available' }}</UBadge>
    </div>

    <div class="grid gap-4 border-b border-default py-6 sm:grid-cols-2">
      <div>
        <h2 class="text-sm font-semibold uppercase tracking-wide text-muted">Content digest</h2>
        <p class="mt-2 break-all font-mono text-sm text-highlighted">{{ resource.content_digest }}</p>
      </div>
      <div>
        <h2 class="text-sm font-semibold uppercase tracking-wide text-muted">Acquisition</h2>
        <p class="mt-2 text-sm text-highlighted">{{ acquisition }}</p>
      </div>
      <div>
        <h2 class="text-sm font-semibold uppercase tracking-wide text-muted">License</h2>
        <p class="mt-2 text-sm text-highlighted">{{ resource.license || 'Not provided' }}</p>
      </div>
      <div>
        <h2 class="text-sm font-semibold uppercase tracking-wide text-muted">Analyzer</h2>
        <p class="mt-2 text-sm text-highlighted">{{ resource.analyzer_version || 'Not provided' }}</p>
      </div>
    </div>

    <section v-if="revoked" role="alert" aria-live="assertive" class="border-b border-default py-6" aria-labelledby="revocation-heading">
      <h2 id="revocation-heading" class="text-lg font-semibold text-error">This revision is revoked</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The Marketplace has marked this exact digest as revoked. This page is read-only; do not install it.</p>
      <p class="mt-3 break-words font-mono text-xs text-dimmed">{{ formatValue(resource.revocation) }}</p>
    </section>

    <section class="border-b border-default py-6" aria-labelledby="provenance-heading">
      <h2 id="provenance-heading" class="text-lg font-semibold text-highlighted">Provenance</h2>
      <dl v-if="provenanceEntries.length" class="mt-4 divide-y divide-default rounded-xl border border-default">
        <div v-for="([key, value], index) in provenanceEntries" :key="`${key}-${index}`" class="grid gap-1 px-4 py-3 sm:grid-cols-[minmax(8rem,0.35fr)_1fr] sm:gap-4">
          <dt class="break-words text-sm font-medium text-muted">{{ key }}</dt>
          <dd class="break-words text-sm text-highlighted">{{ formatValue(value) }}</dd>
        </div>
      </dl>
      <p v-else class="mt-3 text-sm text-muted">No provenance metadata was provided.</p>
    </section>

    <section class="border-b border-default py-6" aria-labelledby="findings-heading">
      <h2 id="findings-heading" class="text-lg font-semibold text-highlighted">Security findings</h2>
      <ul v-if="resource.findings?.length" class="mt-4 space-y-3">
        <li v-for="(finding, index) in resource.findings" :key="index" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-highlighted">
          {{ formatValue(finding) }}
        </li>
      </ul>
      <p v-else class="mt-3 text-sm text-muted">No findings were reported for this revision.</p>
    </section>

    <section class="pt-6" aria-labelledby="acquisition-heading">
      <h2 id="acquisition-heading" class="text-lg font-semibold text-highlighted">Copy exact pin</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The command and browser URL include this revision's content digest. Installation remains a local CLI action.</p>
      <div class="mt-4 grid gap-3">
        <code v-if="actionsAvailable" class="block break-all rounded-xl bg-muted px-4 py-3 font-mono text-sm text-highlighted">{{ command }}</code>
        <p v-if="!actionsAvailable" role="status" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-warning">Actions unavailable: the Marketplace returned an invalid route value, so no locator was generated.</p>
        <div class="flex flex-wrap gap-2">
          <UButton size="sm" color="primary" variant="soft" icon="i-lucide-terminal" :aria-label="`Copy pinned CLI install command for ${name}`" :disabled="!actionsAvailable" @click="copy(command, 'command')">
            {{ copied === 'command' ? 'Copied command' : 'Copy pinned command' }}
          </UButton>
          <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-link" :aria-label="`Copy pinned browser URL for ${name}`" :disabled="!actionsAvailable" @click="copy(browserUrl, 'url')">
            {{ copied === 'url' ? 'Copied URL' : 'Copy pinned URL' }}
          </UButton>
        </div>
        <p v-if="actionsAvailable && copyError" role="alert" class="text-sm text-error">Clipboard access failed. Copy the displayed value manually.</p>
        <p class="sr-only" aria-live="polite">{{ copied === 'command' ? 'Pinned CLI install command copied.' : copied === 'url' ? 'Pinned browser URL copied.' : '' }}</p>
      </div>
    </section>
  </article>
</template>
