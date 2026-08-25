<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { resourceInspectionFilePath } from '~/lib/api'
import { installCommand, kindIcon, kindLabel, resourceLocator, sourceLabel } from '~/lib/presentation'
import { useCopyText } from '~/composables/useCopyText'
import { escapeRawText, renderSafeMarkdown } from '~/lib/markdown'
import type { InspectionFile, InspectionManifest, ResourceKind, ResourceListItem, ResourceRevision } from '~/types/marketplace'

type Props = {
  resource: ResourceRevision
  source: string
  kind: ResourceKind
  name: string
  canonicalUrl: string
  apiBase?: string
  inspection?: InspectionManifest
  primaryContent?: string
  loadInspectionFile?: (file: InspectionFile) => Promise<string | Blob>
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
const revoked = computed(() => Boolean(props.resource.revocation && Object.keys(props.resource.revocation).length))
const commandAvailable = computed(() => !revoked.value && Boolean(command.value))
const browserAvailable = computed(() => Boolean(browserUrl.value))
const acquisition = computed(() => props.resource.acquisition || valueFromRecord(props.resource.provenance, 'acquisition') || 'Not specified')
const provenanceEntries = computed(() => Object.entries(props.resource.provenance ?? {}))
const inspectionFindings = computed(() => props.inspection?.findings ?? [])
const primaryFile = computed(() => props.inspection?.files.find(file => file.path === props.inspection?.primary_path))
const selectedDigest = ref('')
const loadedContents = ref<Record<string, string>>({})
const unavailableContents = ref<Record<string, boolean>>({})
const loadingDigest = ref('')
const selectedFile = computed(() => props.inspection?.files.find(file => file.content_digest === selectedDigest.value) ?? primaryFile.value)
const selectedIsPrimary = computed(() => selectedFile.value?.path === props.inspection?.primary_path)
const selectedIsMarkdown = computed(() => selectedFile.value?.media_type === 'text/markdown')
const selectedIsImage = computed(() => selectedIsPrimary.value && selectedFile.value?.inspection_class === 'image')
const selectedImageURL = computed(() => selectedFile.value && selectedIsImage.value
  ? resourceInspectionFilePath(props.apiBase, props.source, props.kind, props.name, props.resource.content_digest, selectedFile.value.content_digest)
  : '')
const selectedContent = computed(() => {
  if (!selectedFile.value || selectedFile.value.inspection_class !== 'text') return undefined
  if (selectedIsPrimary.value) return props.primaryContent
  return loadedContents.value[selectedFile.value.content_digest]
})
const selectedContentUnavailable = computed(() => Boolean(selectedFile.value && selectedFile.value.inspection_class === 'text' && (
  unavailableContents.value[selectedFile.value.content_digest] || (selectedIsPrimary.value && props.primaryContent === undefined)
)))
const renderedContent = computed(() => selectedContent.value === undefined ? '' : renderSafeMarkdown(selectedContent.value))
const escapedContent = computed(() => selectedContent.value === undefined ? '' : escapeRawText(selectedContent.value))
const textFiles = computed(() => props.inspection?.files.filter(file => file.inspection_class === 'text' && file.path !== props.inspection?.primary_path) ?? [])
const selectableFiles = computed(() => {
  const primary = primaryFile.value
  return primary ? [primary, ...textFiles.value] : textFiles.value
})
const imageFiles = computed(() => props.inspection?.files.filter(file => file.inspection_class === 'image' && file.path !== props.inspection?.primary_path) ?? [])
const binaryFiles = computed(() => props.inspection?.files.filter(file => file.inspection_class === 'metadata') ?? [])

watch(() => props.inspection?.revision_digest, () => {
  selectedDigest.value = primaryFile.value?.content_digest ?? ''
  loadedContents.value = {}
  unavailableContents.value = {}
}, { immediate: true })

watch(selectedDigest, digest => {
  if (!digest || digest === primaryFile.value?.content_digest) return
  const file = props.inspection?.files.find(candidate => candidate.content_digest === digest)
  if (!file || file.inspection_class !== 'text' || loadedContents.value[digest] !== undefined || unavailableContents.value[digest]) return
  if (!props.loadInspectionFile) {
    unavailableContents.value = { ...unavailableContents.value, [digest]: true }
    return
  }
  loadingDigest.value = digest
  void props.loadInspectionFile(file).then(content => {
    if (typeof content !== 'string') throw new Error('inspection text response was not text')
    loadedContents.value = { ...loadedContents.value, [digest]: content }
  }).catch(() => {
    unavailableContents.value = { ...unavailableContents.value, [digest]: true }
  }).finally(() => {
    if (loadingDigest.value === digest) loadingDigest.value = ''
  })
})

function inspectionFileURL(digest: string): string {
  return resourceInspectionFilePath(props.apiBase, props.source, props.kind, props.name, props.resource.content_digest, digest)
}

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

    <section v-if="inspection" class="border-b border-default py-6" aria-labelledby="inspection-heading">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 id="inspection-heading" class="text-lg font-semibold text-highlighted">Inspection preview</h2>
        <span class="font-mono text-xs text-muted">{{ inspection.primary_path }} · {{ inspection.files.length }} file{{ inspection.files.length === 1 ? '' : 's' }}</span>
      </div>
      <div v-if="selectableFiles.length > 1" class="mt-4 rounded-xl border border-default bg-muted/30 p-4">
        <label for="inspection-file" class="text-sm font-medium text-highlighted">Inspect file</label>
        <select id="inspection-file" v-model="selectedDigest" aria-label="Inspection file" class="mt-2 block w-full rounded-lg border border-default bg-default px-3 py-2 text-sm text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
          <option v-for="file in selectableFiles" :key="file.content_digest" :value="file.content_digest">{{ file.path }} · {{ file.size }} bytes</option>
        </select>
        <p class="mt-2 text-xs text-muted">The primary file is selected by default. Additional files load only after selection.</p>
      </div>
      <div v-if="selectedIsImage && selectedImageURL" class="mt-4 rounded-xl border border-default bg-default p-5">
        <img :src="selectedImageURL" alt="Contained resource preview" class="max-h-96 max-w-full object-contain" loading="lazy" referrerpolicy="no-referrer">
      </div>
      <div v-else-if="selectedContent !== undefined && selectedIsMarkdown" class="mt-4 rounded-xl border border-default bg-default p-5">
        <div class="prose prose-sm max-w-none text-default" data-testid="safe-markdown" v-html="renderedContent" />
      </div>
      <pre v-else-if="selectedContent !== undefined" data-testid="escaped-primary" class="mt-4 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-xl border border-default bg-muted/30 p-5 font-mono text-xs text-highlighted" v-html="escapedContent" />
      <p v-else-if="loadingDigest === selectedFile?.content_digest" class="mt-4 rounded-xl border border-default bg-muted/30 px-4 py-3 text-sm text-muted" role="status" aria-live="polite">Loading {{ selectedFile.path }}…</p>
      <p v-else-if="selectedContentUnavailable || !selectedIsImage" class="mt-4 rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-warning" role="status">{{ selectedIsPrimary ? 'Primary content is unavailable or intentionally withheld for safe inspection.' : 'This file is unavailable for safe inspection.' }}</p>
      <div v-if="imageFiles.length" class="mt-4 rounded-xl border border-default bg-muted/30 p-4">
        <p class="text-sm font-medium text-highlighted">Contained raster previews</p>
        <div class="mt-3 grid gap-3 sm:grid-cols-2">
          <figure v-for="file in imageFiles" :key="file.path" class="rounded-lg border border-default bg-default p-3">
            <img :src="inspectionFileURL(file.content_digest)" :alt="`Contained preview ${file.path}`" class="max-h-64 max-w-full object-contain" loading="lazy" referrerpolicy="no-referrer">
            <figcaption class="mt-2 break-all font-mono text-xs text-muted">{{ file.path }} · {{ file.content_digest }}</figcaption>
          </figure>
        </div>
      </div>
      <div v-if="textFiles.length" class="mt-4 rounded-xl border border-default bg-muted/30 px-4 py-3 text-sm text-muted">
        <p class="font-medium text-highlighted">Additional text files are available by immutable digest.</p>
        <ul class="mt-2 space-y-1 font-mono text-xs"><li v-for="file in textFiles" :key="file.path">{{ file.path }} · {{ file.content_digest }} · {{ file.size }} bytes</li></ul>
      </div>
      <div v-if="binaryFiles.length" class="mt-4 rounded-xl border border-default bg-muted/30 px-4 py-3 text-sm text-muted">
        <p class="font-medium text-highlighted">Binary metadata (not rendered)</p>
        <ul class="mt-2 space-y-1 font-mono text-xs"><li v-for="file in binaryFiles" :key="file.path">{{ file.path }} · {{ file.media_type }} · {{ file.content_digest }} · {{ file.size }} bytes</li></ul>
      </div>
      <details v-if="selectedContent !== undefined" class="mt-4 rounded-xl border border-default bg-muted/30">
        <summary class="cursor-pointer px-4 py-3 text-sm font-medium text-highlighted">View escaped raw text</summary>
        <pre :data-testid="selectedIsPrimary ? 'escaped-raw' : 'escaped-selected-raw'" class="max-h-96 overflow-auto whitespace-pre-wrap break-words px-4 pb-4 font-mono text-xs text-highlighted" v-html="escapedContent" />
      </details>
      <ul v-if="inspectionFindings.length" class="mt-4 space-y-3" aria-label="Inspection findings">
        <li v-for="(finding, index) in inspectionFindings" :key="`${finding.code}-${index}`" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-highlighted">
          <span class="font-semibold">{{ finding.code }}</span><span v-if="finding.path" class="ml-2 font-mono text-xs text-muted">{{ finding.path }}:{{ finding.start_byte ?? 0 }}-{{ finding.end_byte ?? finding.start_byte ?? 0 }}</span>
          <p v-if="finding.message" class="mt-1">{{ finding.message }}</p>
        </li>
      </ul>
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
        <code v-if="commandAvailable" class="block break-all rounded-xl bg-muted px-4 py-3 font-mono text-sm text-highlighted">{{ command }}</code>
        <p v-if="revoked" role="status" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-warning">Installation is disabled because this revision is revoked. Metadata and inspection remain available.</p>
        <p v-else-if="!commandAvailable" role="status" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-warning">Actions unavailable: the Marketplace returned an invalid route value, so no locator was generated.</p>
        <div class="flex flex-wrap gap-2">
          <UButton size="sm" color="primary" variant="soft" icon="i-lucide-terminal" :aria-label="`Copy pinned CLI install command for ${name}`" :disabled="!commandAvailable" @click="copy(command, 'command')">
            {{ copied === 'command' ? 'Copied command' : 'Copy pinned command' }}
          </UButton>
          <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-link" :aria-label="`Copy pinned browser URL for ${name}`" :disabled="!browserAvailable" @click="copy(browserUrl, 'url')">
            {{ copied === 'url' ? 'Copied URL' : 'Copy pinned URL' }}
          </UButton>
        </div>
        <p v-if="(commandAvailable || browserAvailable) && copyError" role="alert" class="text-sm text-error">Clipboard access failed. Copy the displayed value manually.</p>
        <p class="sr-only" aria-live="polite">{{ copied === 'command' ? 'Pinned CLI install command copied.' : copied === 'url' ? 'Pinned browser URL copied.' : '' }}</p>
      </div>
    </section>
  </article>
</template>
