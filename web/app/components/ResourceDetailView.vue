<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { resourceInspectionFilePath } from '~/lib/api'
import { installCommand, kindLabel, mergeFindings, resourceAvailability, resourceAvailabilityCopy, resourceLocator, sourceLabel } from '~/lib/presentation'
import { useCopyText } from '~/composables/useCopyText'
import { highlightEscapedRawText, renderSafeMarkdown } from '~/lib/markdown'
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
  revisions?: ResourceRevision[]
  revisionsError?: boolean
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
// Install status is derived from what the Marketplace actually reports:
// revocation, reject-severity analyzer findings, and whether an installable
// artifact exists. Every state stays browsable and inspectable.
const availability = computed(() => resourceAvailability(props.resource))
const availabilityCopy = computed(() => resourceAvailabilityCopy(props.resource))
// Saturation carries the verdict and nothing else, so the status marker is the
// one coloured element in the header.
const statusDotClass = computed(() => {
  switch (availabilityCopy.value.color) {
    case 'success': return 'bg-success'
    case 'error': return 'bg-error'
    default: return 'bg-warning'
  }
})
const revoked = computed(() => availability.value === 'revoked')
const commandAvailable = computed(() => availabilityCopy.value.installable && Boolean(command.value))
const installNotice = computed(() => availabilityCopy.value.installNotice
  || (command.value ? '' : 'Actions unavailable: the Marketplace returned an invalid route value, so no locator was generated.'))
const browserAvailable = computed(() => Boolean(browserUrl.value))
const acquisition = computed(() => props.resource.acquisition || valueFromRecord(props.resource.provenance, 'acquisition') || 'Not specified')
const provenanceEntries = computed(() => Object.entries(props.resource.provenance ?? {}))
const showRevisionHistory = computed(() => props.revisions !== undefined)
const revisionHistory = computed(() => {
  if (!props.revisions) return []
  const seen = new Set<string>()
  return [...props.revisions, props.resource].filter(revision => {
    if (seen.has(revision.content_digest)) return false
    seen.add(revision.content_digest)
    return true
  })
})
// The revision record and the inspection manifest project the same analyzer
// run and in practice repeat each other. Merge them into one list so a reader
// sees each finding once; the manifest goes first because it carries the file
// path and byte range that the revision record drops.
const findings = computed(() => mergeFindings(props.inspection?.findings, props.resource.findings))
const locatedFindings = computed(() => findings.value.filter(finding => finding.path !== ''))
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
const selectedFindingDetails = computed(() => locatedFindings.value.filter(finding => finding.path === selectedFile.value?.path))
const renderedContent = computed(() => selectedContent.value === undefined ? '' : renderSafeMarkdown(selectedContent.value))
const highlightedContent = computed(() => selectedContent.value === undefined ? '' : highlightEscapedRawText(selectedContent.value, selectedFindingDetails.value.map(finding => ({
  index: finding.index,
  startByte: finding.startByte,
  endByte: finding.endByte,
  label: `${finding.code} (${finding.severity})`,
}))))
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

function revisionURL(revision: ResourceRevision): string {
  return resourceLocator({
    source_id: props.source,
    kind: props.kind,
    name: props.name,
    revision: revision.content_digest,
    content_digest: revision.content_digest,
  }, props.canonicalUrl)
}

function isCurrentRevision(revision: ResourceRevision): boolean {
  return revision.content_digest === props.resource.content_digest
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
  <article class="border border-default bg-default p-5 sm:p-7">
    <div class="flex flex-wrap items-start justify-between gap-4 border-b border-default pb-5">
      <div class="flex min-w-0 items-start gap-4">
        <!-- The same mark the index row carries, at reading size. -->
        <ResourceFingerprint :digest="resource.content_digest" :size="44" class="mt-1" />
        <div class="min-w-0">
          <p class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">{{ kindLabel(kind) }}</p>
          <h1 class="mt-1 break-words font-mono text-2xl tracking-tight">
            <span class="text-muted">{{ sourceLabel(listItem) }}/</span><span class="font-medium text-highlighted">{{ name }}</span>
          </h1>
        </div>
      </div>
      <span class="inline-flex shrink-0 items-center gap-2 text-sm">
        <span :class="statusDotClass" class="size-2 rounded-full" aria-hidden="true" />
        <span class="text-toned">{{ availabilityCopy.label }}</span>
      </span>
    </div>
    <p class="sr-only" aria-live="polite">Install status for {{ name }}: {{ availabilityCopy.label }}.</p>

    <!-- Provenance reads as a record, so it is set as label/value pairs in mono
         rather than as prose. -->
    <dl class="grid gap-x-8 gap-y-4 border-b border-default py-5 sm:grid-cols-2">
      <div class="sm:col-span-2">
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Content digest</dt>
        <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ resource.content_digest }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Acquisition</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ acquisition }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">License</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ resource.license || 'none declared' }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Analyzer</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ resource.analyzer_version || 'not recorded' }}</dd>
      </div>
    </dl>

    <section v-if="revoked" role="alert" aria-live="assertive" class="border-b border-default py-6" aria-labelledby="revocation-heading">
      <h2 id="revocation-heading" class="text-lg font-semibold text-error">This revision is revoked</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The Marketplace has marked this exact digest as revoked. This page is read-only; do not install it.</p>
      <p class="mt-3 break-words font-mono text-xs text-dimmed">{{ formatValue(resource.revocation) }}</p>
    </section>

    <section v-else-if="availability === 'policy-blocked'" role="alert" aria-live="assertive" class="border-b border-default py-6" aria-labelledby="policy-block-heading">
      <h2 id="policy-block-heading" class="text-lg font-semibold text-error">The security policy rejected this revision</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The analyzer reported findings at reject severity, so this Marketplace kept the revision indexed for inspection and removed its installable artifact. Read the metadata, inspection preview, and findings below. There is nothing to install.</p>
    </section>

    <section v-if="showRevisionHistory" class="border-b border-default py-6" aria-labelledby="revision-history-heading">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <h2 id="revision-history-heading" class="text-lg font-semibold text-highlighted">Revision history</h2>
        <span class="text-xs text-muted">{{ revisionHistory.length }} revision{{ revisionHistory.length === 1 ? '' : 's' }}</span>
      </div>
      <p v-if="revisionsError" class="mt-3 rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-warning" role="status" aria-live="polite">Revision history is unavailable. The current revision remains available below.</p>
      <ol v-else class="mt-4 space-y-2" aria-label="Resource revision history">
        <li v-for="revision in revisionHistory" :key="revision.content_digest" class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-default bg-muted/30 px-4 py-3">
          <div class="min-w-0">
            <NuxtLink v-if="revisionURL(revision)" :to="revisionURL(revision)" class="break-all font-mono text-sm text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">{{ revision.content_digest }}</NuxtLink>
            <span v-else class="break-all font-mono text-sm text-muted">{{ revision.content_digest }}</span>
            <span v-if="isCurrentRevision(revision)" class="ml-2 text-xs font-medium text-muted">Current</span>
          </div>
          <UBadge :color="resourceAvailabilityCopy(revision).color" variant="subtle">{{ resourceAvailabilityCopy(revision).label }}</UBadge>
        </li>
      </ol>
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
      <pre v-else-if="selectedContent !== undefined" data-testid="escaped-primary" aria-label="Escaped raw inspection text" class="mt-4 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-xl border border-default bg-muted/30 p-5 font-mono text-xs text-highlighted" v-html="highlightedContent" />
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
        <p v-if="selectedFindingDetails.some(finding => finding.startByte !== undefined && finding.endByte !== undefined)" class="px-4 pb-2 text-xs text-muted">Finding ranges are highlighted below. Select a finding's range link to move focus to its highlighted text.</p>
        <pre :data-testid="selectedIsPrimary ? 'escaped-raw' : 'escaped-selected-raw'" aria-label="Escaped raw inspection text" class="max-h-96 overflow-auto whitespace-pre-wrap break-words px-4 pb-4 font-mono text-xs text-highlighted" v-html="highlightedContent" />
      </details>
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
      <p v-if="findings.length" class="mt-2 text-sm leading-6 text-muted">Every finding the analyzer reported for this revision, listed once. The revision record and the inspection manifest report the same analyzer run.</p>
      <ul v-if="findings.length" class="mt-4 space-y-3" aria-label="Security findings">
        <li v-for="finding in findings" :id="`finding-${finding.index}`" :key="finding.index" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-highlighted">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span class="font-semibold">{{ finding.code }}</span>
            <span class="font-medium text-warning">Severity: {{ finding.severity }}</span>
            <span v-if="finding.policy" class="text-muted">Policy: {{ finding.policy }}</span>
          </div>
          <p v-if="finding.path" class="mt-1 font-mono text-xs text-muted">{{ finding.path }}:{{ finding.startByte ?? 0 }}-{{ finding.endByte ?? finding.startByte ?? 0 }}</p>
          <p v-if="finding.message" class="mt-1">{{ finding.message }}</p>
          <a v-if="selectedContent !== undefined && finding.path === selectedFile?.path && finding.startByte !== undefined && finding.endByte !== undefined" :href="`#finding-highlight-${finding.index}`" class="mt-2 inline-block text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">View highlighted range</a>
        </li>
      </ul>
      <p v-else class="mt-3 text-sm text-muted">No findings were reported for this revision.</p>
    </section>

    <section class="pt-6" aria-labelledby="acquisition-heading">
      <h2 id="acquisition-heading" class="text-lg font-semibold text-highlighted">Copy exact pin</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The command and browser URL include this revision's content digest. Installation remains a local CLI action.</p>
      <div class="mt-4 grid gap-3">
        <code v-if="commandAvailable" class="block break-all rounded-xl bg-muted px-4 py-3 font-mono text-sm text-highlighted">{{ command }}</code>
        <p v-if="installNotice" role="status" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-warning">{{ installNotice }}</p>
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
