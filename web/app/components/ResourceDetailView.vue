<script setup lang="ts">
import { computed } from 'vue'
import CopyButton from '~/components/CopyButton.vue'
import { installCommand, kindLabel, resourceAvailability, resourceAvailabilityCopy, resourceLocator, resourceRoute, registryLabel } from '~/lib/presentation'
import { useCopyText } from '~/composables/useCopyText'
import { useResourceInspectionPreview } from '~/composables/useResourceInspectionPreview'
import type { InspectionFile, InspectionManifest, ResourceKind, ResourceListItem, ResourceRevision } from '~/types/marketplace'

type Props = {
  resource: ResourceRevision
  registry: string
  acquisition?: string
  kind: ResourceKind
  name: string
  canonicalUrl: string
  browserBase?: string
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
  registry_id: props.registry,
  kind: props.kind,
  name: props.name,
  revision: props.resource.content_digest,
  content_digest: props.resource.content_digest,
}))
const browserUrl = computed(() => resourceLocator(listItem.value, props.browserBase ?? props.canonicalUrl))
const command = computed(() => installCommand(listItem.value, props.canonicalUrl))
// Install status is derived from what the Marketplace actually reports:
// revocation, the nested assessment decision, and whether an installable
// artifact exists. Every state stays browsable and inspectable.
const availability = computed(() => resourceAvailability(props.resource, props.acquisition, props.kind))
const availabilityCopy = computed(() => resourceAvailabilityCopy(props.resource, props.acquisition, props.kind))
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
const occurrenceEntries = computed(() => Object.entries(props.resource.occurrence))
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
const {
  binaryFiles,
  displayFindings,
  findings,
  highlightedContent,
  imageFiles,
  inspectionFileURL,
  loadingDigest,
  renderedContent,
  selectableFiles,
  selectedContent,
  selectedContentUnavailable,
  selectedDigest,
  selectedFile,
  selectedFindingDetails,
  selectedImageURL,
  selectedIsImage,
  selectedIsMarkdown,
  selectedIsPrimary,
  textFiles,
} = useResourceInspectionPreview(props)

function revisionURL(revision: ResourceRevision): string {
  return resourceRoute({
    registry_id: props.registry,
    kind: props.kind,
    name: props.name,
    revision: revision.content_digest,
    content_digest: revision.content_digest,
  })
}

function isCurrentRevision(revision: ResourceRevision): boolean {
  return revision.content_digest === props.resource.content_digest
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
            <span class="text-muted">{{ registryLabel(listItem) }}/</span><span class="font-medium text-highlighted">{{ name }}</span>
          </h1>
          <p v-if="resource.description" class="mt-2 text-sm leading-6 text-muted">{{ resource.description }}</p>
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
      <div class="sm:col-span-2">
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Archive digest</dt>
        <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ resource.archive_digest || 'not stored' }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Registry commit</dt>
        <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ resource.occurrence.commit }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">License</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ resource.license || 'none declared' }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Analyzer</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ resource.assessment.analyzer_version }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Assessment digest</dt>
        <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ resource.assessment.assessment_digest }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Assessment decision</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ resource.assessment.decision }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Policy digest</dt>
        <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ resource.assessment.policy_digest }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Assessed at</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ resource.assessment.assessed_at }}</dd>
      </div>
    </dl>

    <section v-if="revoked" role="alert" aria-live="assertive" class="border-b border-default py-6" aria-labelledby="revocation-heading">
      <h2 id="revocation-heading" class="text-lg font-semibold text-error">This revision is revoked</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The Marketplace has marked this exact digest as revoked. This page is read-only; do not install it.</p>
      <p class="mt-3 break-words font-mono text-xs text-dimmed">{{ formatValue(resource.revocation) }}</p>
    </section>

    <section v-else-if="availability === 'policy-blocked'" role="alert" aria-live="assertive" class="border-b border-default py-6" aria-labelledby="policy-block-heading">
      <h2 id="policy-block-heading" class="text-lg font-semibold text-error">The security policy rejected this revision</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The authoritative assessment rejected this revision, so this Marketplace keeps it indexed for inspection but blocks its artifact from serving and installation. Read the metadata, inspection preview, and findings below. There is nothing to install.</p>
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
          <UBadge :color="resourceAvailabilityCopy(revision, acquisition, kind).color" variant="subtle">{{ resourceAvailabilityCopy(revision, acquisition, kind).label }}</UBadge>
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
        <div class="marketplace-markdown" data-testid="safe-markdown" v-html="renderedContent" />
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
      <h2 id="provenance-heading" class="text-lg font-semibold text-highlighted">Registry occurrence</h2>
      <dl v-if="occurrenceEntries.length" class="mt-4 divide-y divide-default rounded-xl border border-default">
        <div v-for="([key, value], index) in occurrenceEntries" :key="`${key}-${index}`" class="grid gap-1 px-4 py-3 sm:grid-cols-[minmax(8rem,0.35fr)_1fr] sm:gap-4">
          <dt class="break-words text-sm font-medium text-muted">{{ key }}</dt>
          <dd class="break-words text-sm text-highlighted">{{ formatValue(value) }}</dd>
        </div>
      </dl>
      <p v-else class="mt-3 text-sm text-muted">No registry occurrence metadata was provided.</p>
    </section>

    <section class="border-b border-default py-6" aria-labelledby="findings-heading">
      <h2 id="findings-heading" class="text-lg font-semibold text-highlighted">Security findings</h2>
      <p v-if="findings.length" class="mt-2 text-sm leading-6 text-muted">Every finding reported by this revision's assessment.</p>
      <ul v-if="findings.length" class="mt-4 space-y-3" aria-label="Security findings">
        <li v-for="finding in displayFindings" :id="`finding-${finding.index}`" :key="finding.index" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-highlighted">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span class="font-semibold">{{ finding.code }}</span>
            <span class="font-medium text-warning">Severity: {{ finding.severity }}</span>
            <span v-if="finding.policy" class="text-muted">Policy: {{ finding.policy }}</span>
          </div>
          <p v-if="finding.path" class="mt-1 font-mono text-xs text-muted">{{ finding.path }}<template v-if="finding.startByte !== undefined">:{{ finding.startByte }}-{{ finding.endByte ?? finding.startByte }}</template><template v-else-if="finding.line !== undefined">:{{ finding.line }}<template v-if="finding.column !== undefined">:{{ finding.column }}</template></template></p>
          <p v-if="finding.message" class="mt-1">{{ finding.message }}</p>
          <a v-if="selectedContent !== undefined && finding.path === selectedFile?.path && finding.startByte !== undefined && finding.endByte !== undefined" :href="`#finding-highlight-${finding.index}`" class="mt-2 inline-block text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">View highlighted range</a>
        </li>
      </ul>
      <p v-else class="mt-3 text-sm text-muted">No findings were reported for this revision.</p>
    </section>

    <section class="pt-6" aria-labelledby="acquisition-heading">
      <h2 id="acquisition-heading" class="text-lg font-semibold text-highlighted">Install exact revision</h2>
      <p class="mt-2 text-sm leading-6 text-muted">Copy the digest-pinned command to install this exact revision. Copy the detail URL to share this page. Installation remains a local CLI action.</p>
      <div class="mt-4 grid gap-3">
        <CommandBlock :command="command" :label="`pinned CLI install command for ${name}`" :disabled="!commandAvailable" />
        <p v-if="installNotice" role="status" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-warning">{{ installNotice }}</p>
        <div class="flex flex-wrap gap-2">
          <CopyButton
            :copied="copied === 'url'"
            :copy-label="`Copy detail URL for ${name}`"
            :copied-label="`Detail URL copied for ${name}`"
            copy-text="Copy detail URL"
            copied-text="Detail URL copied"
            :disabled="!browserAvailable"
            @click="copy(browserUrl, 'url')"
          />
        </div>
        <p v-if="browserAvailable && copyError === 'url'" role="alert" class="text-sm text-error">Clipboard access failed. Copy the displayed value manually.</p>
        <p class="sr-only" aria-live="polite">{{ copied === 'url' ? 'Detail URL copied.' : '' }}</p>
      </div>
    </section>
  </article>
</template>
