<script setup lang="ts">
import { computed, ref } from 'vue'
import CopyButton from '~/components/CopyButton.vue'
import { useCopyText } from '~/composables/useCopyText'
import { useResourceInspectionPreview } from '~/composables/useResourceInspectionPreview'
import { isResourceKind } from '~/lib/api'
import { DOCS_PATH } from '~/lib/site'
import { installCommand, kindLabel, resourceAvailability, resourceAvailabilityCopy, resourceLatestRoute, resourceRoute, shortDigest } from '~/lib/presentation'
import type { InspectionFile, InspectionManifest, Marketplace, Registry, ResourceKind, ResourceListItem, ResourceRevision } from '~/types/marketplace'

type Props = {
  resource: ResourceRevision
  registry: string
  acquisition?: string
  kind: ResourceKind
  name: string
  canonicalUrl: string
  browserBase: string
  apiBase?: string
  inspection?: InspectionManifest
  primaryContent?: string
  loadInspectionFile?: (file: InspectionFile) => Promise<string | Blob>
  revisions?: ResourceRevision[]
  revisionsError?: boolean
  currentRevisionDigest?: string
  relatedResources?: ResourceListItem[]
  registryDetails?: Registry
  operator?: Marketplace
  initialTab?: Tab
}

type Tab = 'content' | 'security' | 'revisions' | 'registry' | 'technical'
type ContentMode = 'preview' | 'raw'
type TechnicalEntry = { label: string, value: string, copyable?: boolean }
type CollectionMember = { kind: ResourceKind, name: string, description?: string, to: string }

const props = defineProps<Props>()
const activeTab = ref<Tab>(props.initialTab || 'content')
const contentMode = ref<ContentMode>('preview')
const { copied, copy } = useCopyText()

const listItem = computed<ResourceListItem>(() => ({
  registry_id: props.registry,
  kind: props.kind,
  name: props.name,
  revision: props.resource.content_digest,
  content_digest: props.resource.content_digest,
}))
const command = computed(() => installCommand(listItem.value, props.canonicalUrl))
// Install status is derived from what the Marketplace actually reports:
// revocation, the nested assessment decision, and whether an installable
// artifact exists. Every state stays browsable and inspectable.
const availability = computed(() => resourceAvailability(props.resource, props.acquisition, props.kind))
const availabilityCopy = computed(() => resourceAvailabilityCopy(props.resource, props.acquisition, props.kind))
const revoked = computed(() => availability.value === 'revoked')
const commandAvailable = computed(() => availabilityCopy.value.installable && Boolean(command.value))
const installNotice = computed(() => availabilityCopy.value.installNotice
  || (command.value ? '' : 'Actions unavailable: the Marketplace returned an invalid route value, so no locator was generated.'))
const selectedIsCurrent = computed(() => props.resource.content_digest === (props.currentRevisionDigest || props.resource.content_digest))
const currentRevision = computed(() => props.currentRevisionDigest || props.resource.content_digest)
const currentRevisionURL = computed(() => resourceRoute({ ...listItem.value, revision: currentRevision.value, content_digest: currentRevision.value }))
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
  selectedImageURL,
  selectedIsImage,
  selectedIsMarkdown,
  selectedIsPrimary,
} = useResourceInspectionPreview(props)
const collectionMembers = computed<CollectionMember[] | undefined>(() => parseCollectionMembers(selectedIsPrimary.value ? selectedContent.value : undefined))
const hasContentPreview = computed(() => selectedIsImage.value || selectedIsMarkdown.value || collectionMembers.value !== undefined)
const assessmentDecision = computed(() => {
  switch (props.resource.assessment.decision) {
    case 'allow': return 'Allowed by marketplace policy'
    case 'warn': return 'Allowed with marketplace warnings'
    default: return 'Rejected by marketplace policy'
  }
})
const assessedAt = computed(() => formatDate(props.resource.assessment.assessed_at))
const technicalEntries = computed<TechnicalEntry[]>(() => {
  const occurrence = props.resource.occurrence as Record<string, unknown>
  const coordinate = props.resource.coordinate
  const entries: TechnicalEntry[] = [
    { label: 'Content digest', value: props.resource.content_digest, copyable: true },
    { label: 'Archive digest', value: props.resource.archive_digest || 'Not stored', copyable: Boolean(props.resource.archive_digest) },
    { label: 'Registry commit', value: props.resource.occurrence.commit, copyable: true },
    { label: 'Manifest digest', value: props.resource.occurrence.manifest_digest, copyable: true },
    { label: 'Marketplace', value: coordinate.marketplace },
    { label: 'Registry', value: coordinate.registry },
    { label: 'Kind', value: coordinate.kind },
    { label: 'Name', value: coordinate.name },
    { label: 'License', value: props.resource.license || 'None declared' },
  ]
  const known = new Set(['coordinate', 'content_digest', 'commit', 'manifest_digest'])
  for (const [key, value] of Object.entries(occurrence)) {
    if (!known.has(key)) entries.push({ label: key, value: formatValue(value) })
  }
  return entries
})

function parseCollectionMembers(content: string | undefined): CollectionMember[] | undefined {
  if (props.kind !== 'collection' || content === undefined) return undefined
  try {
    const manifest = JSON.parse(content) as unknown
    if (!manifest || typeof manifest !== 'object' || Array.isArray(manifest)) return undefined
    const record = manifest as Record<string, unknown>
    const members = Array.isArray(record.members) ? record.members : Array.isArray(record.resources) ? record.resources : undefined
    if (!members) return undefined
    return members.slice(0, 100).flatMap(value => {
      if (!value || typeof value !== 'object' || Array.isArray(value)) return []
      const member = value as Record<string, unknown>
      if (typeof member.kind !== 'string' || !isResourceKind(member.kind) || typeof member.name !== 'string' || typeof member.registry_id !== 'string') return []
      const related = props.relatedResources?.find(resource => resource.kind === member.kind && resource.name === member.name && (resource.registry_id === member.registry_id || resource.registry_slug === member.registry_id))
      const item: ResourceListItem = {
        registry_id: member.registry_id,
        kind: member.kind,
        name: member.name,
        revision: typeof member.revision === 'string' ? member.revision : related?.revision || '',
        content_digest: typeof member.revision === 'string' ? member.revision : related?.content_digest || '',
      }
      const description = typeof member.description === 'string' ? member.description : related?.description
      return [{ kind: member.kind, name: member.name, ...(description ? { description } : {}), to: resourceRoute(item) || resourceLatestRoute(item) }]
    })
  } catch {
    return undefined
  }
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeZone: 'UTC' }).format(date)
}

function shortIdentifier(value: string): string {
  return shortDigest(value).replace(/^sha256:/, '')
}

function revisionLabel(revision: ResourceRevision): string {
  return shortIdentifier(revision.content_digest)
}

function revisionURL(revision: ResourceRevision): string {
  const route = resourceRoute({
    registry_id: props.registry,
    kind: props.kind,
    name: props.name,
    revision: revision.content_digest,
    content_digest: revision.content_digest,
  })
  return route ? `${route}?tab=revisions` : ''
}

function isCurrentRevision(revision: ResourceRevision): boolean {
  return revision.content_digest === currentRevision.value
}

function isSelectedRevision(revision: ResourceRevision): boolean {
  return revision.content_digest === props.resource.content_digest
}

function revisionStatusClass(revision: ResourceRevision): string {
  const color = resourceAvailabilityCopy(revision, props.acquisition, props.kind).color
  if (color === 'error') return 'text-error'
  if (color === 'warning') return 'text-warning'
  return 'text-muted'
}

function selectTab(tab: Tab) {
  activeTab.value = tab
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
  <article class="mx-auto max-w-6xl">
    <header class="pb-6">
      <div class="flex min-w-0 items-start gap-3 sm:gap-4">
        <!-- The same mark the index row carries, at reading size. -->
        <ResourceFingerprint :digest="resource.content_digest" :size="40" class="mt-1 hidden sm:block" />
        <div class="min-w-0 flex-1">
          <div class="flex min-w-0 flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <h1 class="min-w-0 max-w-full break-words text-2xl font-semibold tracking-tight text-highlighted sm:text-3xl">{{ name }}</h1>
            <span class="shrink-0 text-sm font-medium text-muted">{{ kindLabel(kind) }}</span>
          </div>
          <p v-if="resource.description" class="mt-2 max-w-3xl text-sm leading-6 text-toned sm:text-base">{{ resource.description }}</p>
          <div class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-default pt-3">
            <div>
              <p class="text-xs text-muted">Revision</p>
              <p class="mt-0.5 flex flex-wrap items-center gap-2 text-sm font-medium text-highlighted">
                <span>Scanned {{ assessedAt }}</span>
                <span v-if="selectedIsCurrent" class="text-xs font-normal text-muted">Latest</span>
                <NuxtLink v-else-if="currentRevisionURL" :to="currentRevisionURL" class="rounded-full bg-warning/10 px-2 py-0.5 text-xs font-semibold text-warning hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">Newer revision available</NuxtLink>
              </p>
            </div>
            <div class="flex items-center gap-2">
              <span class="font-mono text-xs text-muted" :title="resource.content_digest">{{ revisionLabel(resource) }}</span>
              <CopyButton :copied="copied === 'revision'" :copy-label="`Copy revision ${revisionLabel(resource)}`" copied-label="Revision copied" icon-only @click="copy(resource.content_digest, 'revision')" />
            </div>
          </div>
        </div>
      </div>
    </header>
    <p class="sr-only" aria-live="polite">Install status for {{ name }}: {{ availabilityCopy.label }}.</p>

    <section data-testid="installation-panel" aria-labelledby="installation-heading">
      <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 id="installation-heading" class="text-sm font-semibold text-highlighted">Install with the CLI</h2>
        <a :href="DOCS_PATH" class="text-sm font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">Set up CLI</a>
      </div>
      <CommandBlock :command="command" :label="`pinned CLI install command for ${name}`" :disabled="!commandAvailable" />
      <div v-if="installNotice" role="status" class="mt-3 border-l-2 border-warning pl-3 text-sm text-toned">
        <p class="font-semibold text-highlighted">Not available</p>
        <p class="mt-1">{{ installNotice }}</p>
      </div>
      <p class="sr-only" aria-live="polite">{{ copied === 'url' ? 'Detail URL copied.' : '' }}</p>
    </section>

    <section v-if="revoked" role="alert" aria-live="assertive" class="mt-4 border-l-2 border-error pl-3" aria-labelledby="revocation-heading">
      <h2 id="revocation-heading" class="text-sm font-semibold text-error">This revision is revoked</h2>
      <p class="mt-1 text-sm leading-6 text-toned">The Marketplace has marked this exact digest as revoked. This page is read-only; do not install it.</p>
      <p v-if="resource.revocation?.reason" class="mt-1 text-sm text-toned">Reason: {{ resource.revocation.reason }}</p>
    </section>
    <section v-else-if="availability === 'policy-blocked'" role="alert" aria-live="assertive" class="mt-4 border-l-2 border-error pl-3" aria-labelledby="policy-block-heading">
      <h2 id="policy-block-heading" class="text-sm font-semibold text-error">The security policy rejected this revision</h2>
      <p class="mt-1 text-sm leading-6 text-toned">The assessment rejected this revision, so it remains available for inspection but cannot be installed.</p>
    </section>

    <div class="mt-7 border-b border-default">
      <div role="tablist" aria-label="Resource details" class="flex gap-1 overflow-x-auto">
        <button v-for="tab in ([['content', 'Content'], ['security', 'Security'], ['revisions', 'Revisions'], ['registry', 'Registry'], ['technical', 'Technical details']] as const)" :id="`${tab[0]}-tab`" :key="tab[0]" type="button" role="tab" :aria-selected="activeTab === tab[0]" :aria-controls="`${tab[0]}-panel`" class="shrink-0 border-b-2 px-3 py-3 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-primary" :class="activeTab === tab[0] ? 'border-primary text-highlighted' : 'border-transparent text-muted hover:text-highlighted'" @click="selectTab(tab[0])">{{ tab[1] }}</button>
      </div>
    </div>

    <section v-if="activeTab === 'content'" id="content-panel" role="tabpanel" aria-labelledby="content-tab" class="py-6">
      <div v-if="inspection">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-default pb-3">
          <div class="min-w-0 flex-1">
            <select v-if="selectableFiles.length > 1" id="inspection-file" v-model="selectedDigest" aria-label="Inspection file" class="block max-w-full rounded border border-[var(--am-color-border-control)] bg-[var(--am-color-bg-control)] px-2.5 py-1.5 font-mono text-sm text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--am-color-action-focus-ring)]">
              <option v-for="file in selectableFiles" :key="file.content_digest" :value="file.content_digest">{{ file.path }}</option>
            </select>
            <p v-else class="truncate font-mono text-sm font-medium text-highlighted" :title="selectedFile?.path">{{ selectedFile?.path || inspection.primary_path }}</p>
          </div>
          <div v-if="hasContentPreview" class="inline-flex shrink-0 rounded border border-default bg-[var(--am-color-bg-subtle)] p-0.5" aria-label="Content view">
            <button type="button" class="rounded px-2.5 py-1 text-xs font-medium" :class="contentMode === 'preview' ? 'bg-[var(--am-color-bg-raised)] text-highlighted shadow-sm' : 'text-muted'" :aria-pressed="contentMode === 'preview'" @click="contentMode = 'preview'">Preview</button>
            <button type="button" class="rounded px-2.5 py-1 text-xs font-medium" :class="contentMode === 'raw' ? 'bg-[var(--am-color-bg-raised)] text-highlighted shadow-sm' : 'text-muted'" :aria-pressed="contentMode === 'raw'" @click="contentMode = 'raw'">Raw</button>
          </div>
        </div>

        <div v-if="collectionMembers !== undefined && contentMode === 'preview'" data-testid="collection-members" class="py-5">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <h2 class="text-lg font-semibold text-highlighted">Collection resources</h2>
            <span class="text-sm text-muted">{{ collectionMembers.length }} resource{{ collectionMembers.length === 1 ? '' : 's' }}</span>
          </div>
          <ul v-if="collectionMembers.length" class="mt-3 divide-y divide-default border-y border-default">
            <li v-for="member in collectionMembers" :key="`${member.kind}:${member.name}:${member.to}`" class="py-3">
              <div class="flex min-w-0 items-baseline gap-3">
                <span class="shrink-0 text-xs font-medium text-muted">{{ kindLabel(member.kind) }}</span>
                <NuxtLink v-if="member.to" :to="member.to" class="min-w-0 truncate font-mono text-sm font-semibold text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">{{ member.name }}</NuxtLink>
                <span v-else class="min-w-0 truncate font-mono text-sm font-semibold text-highlighted">{{ member.name }}</span>
              </div>
              <p v-if="member.description" class="mt-1 text-sm leading-6 text-toned">{{ member.description }}</p>
            </li>
          </ul>
          <p v-else class="mt-3 text-sm text-muted">This collection does not contain any resources.</p>
        </div>
        <div v-else-if="selectedIsImage && selectedImageURL && contentMode === 'preview'" class="py-5">
          <img :src="selectedImageURL" alt="Contained resource preview" class="max-h-96 max-w-full object-contain" loading="lazy" referrerpolicy="no-referrer">
        </div>
        <div v-else-if="selectedContent !== undefined && selectedIsMarkdown && contentMode === 'preview'" class="py-6">
          <div class="marketplace-markdown" data-testid="safe-markdown" v-html="renderedContent" />
        </div>
        <pre v-else-if="selectedContent !== undefined" :data-testid="selectedIsPrimary ? 'escaped-raw' : 'escaped-selected-raw'" aria-label="Escaped raw inspection text" class="my-5 max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded border border-[var(--am-color-code-border)] bg-[var(--am-color-code-bg)] p-4 font-mono text-xs leading-6 text-[var(--am-color-code-text)]" v-html="highlightedContent" />
        <p v-else-if="loadingDigest === selectedFile?.content_digest" class="py-6 text-sm text-toned" role="status" aria-live="polite">Loading {{ selectedFile.path }}…</p>
        <p v-else-if="selectedContentUnavailable || !selectedIsImage" class="my-5 border-l-2 border-warning pl-3 text-sm text-toned" role="status">{{ selectedIsPrimary ? 'Primary content is unavailable or intentionally withheld for safe inspection.' : 'This file is unavailable for safe inspection.' }}</p>
      </div>
      <p v-else class="text-sm text-muted">No resource content is available for preview.</p>
    </section>

    <section v-else-if="activeTab === 'security'" id="security-panel" role="tabpanel" aria-labelledby="security-tab" class="py-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 class="text-lg font-semibold text-highlighted">Automated scan</h2>
          <p class="mt-1 text-sm font-medium text-highlighted">{{ assessmentDecision }}</p>
        </div>
        <time class="text-sm text-muted" :datetime="resource.assessment.assessed_at">Scanned {{ assessedAt }}</time>
      </div>
      <ul v-if="findings.length" class="mt-5 space-y-3" aria-label="Security findings">
        <li v-for="finding in displayFindings" :id="`finding-${finding.index}`" :key="finding.index" class="border-l-2 border-warning pl-4 text-sm text-highlighted">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span class="font-semibold">{{ finding.code }}</span>
            <span class="font-medium text-warning">Severity: {{ finding.severity }}</span>
            <span v-if="finding.policy" class="text-muted">Policy: {{ finding.policy }}</span>
          </div>
          <p v-if="finding.path" class="mt-1 font-mono text-xs text-muted">{{ finding.path }}<template v-if="finding.startByte !== undefined">:{{ finding.startByte }}-{{ finding.endByte ?? finding.startByte }}</template><template v-else-if="finding.line !== undefined">:{{ finding.line }}<template v-if="finding.column !== undefined">:{{ finding.column }}</template></template></p>
          <p v-if="finding.message" class="mt-1">{{ finding.message }}</p>
          <button v-if="selectedContent !== undefined && finding.path === selectedFile?.path && finding.startByte !== undefined && finding.endByte !== undefined" type="button" class="mt-2 text-xs font-medium text-primary underline-offset-4 hover:underline" @click="selectTab('content'); contentMode = 'raw'">View highlighted range</button>
        </li>
      </ul>
      <p v-else class="mt-5 text-sm text-toned"><span class="font-medium text-highlighted">Automated scan: no issues detected.</span> Automated analysis is limited and is not a safety endorsement.</p>
      <details class="mt-6 border-t border-default pt-4">
        <summary class="cursor-pointer text-sm font-medium text-highlighted">Analyzer and assessment parameters</summary>
        <dl class="mt-4 divide-y divide-default border-y border-default">
          <div class="grid gap-2 py-3 sm:grid-cols-[12rem_minmax(0,1fr)_auto] sm:items-center">
            <dt class="text-sm text-muted">Analyzer</dt>
            <dd class="text-sm text-highlighted">{{ resource.assessment.analyzer_version }}</dd>
          </div>
          <div v-for="entry in [{ label: 'Assessment digest', value: resource.assessment.assessment_digest }, { label: 'Policy digest', value: resource.assessment.policy_digest }, { label: 'Inspection manifest digest', value: resource.assessment.inspection_manifest_digest }]" :key="entry.label" class="grid gap-2 py-3 sm:grid-cols-[12rem_minmax(0,1fr)_auto] sm:items-center">
            <dt class="text-sm text-muted">{{ entry.label }}</dt>
            <dd class="font-mono text-sm text-highlighted" :title="entry.value">{{ shortIdentifier(entry.value) }}</dd>
            <CopyButton :copied="copied === `security-${entry.label}`" :copy-label="`Copy ${entry.label}`" :copied-label="`${entry.label} copied`" icon-only @click="copy(entry.value, `security-${entry.label}`)" />
          </div>
        </dl>
      </details>
    </section>

    <section v-else-if="activeTab === 'revisions'" id="revisions-panel" role="tabpanel" aria-labelledby="revisions-tab" class="py-6">
      <div class="flex flex-wrap items-baseline justify-between gap-3">
        <div>
          <h2 id="revision-history-heading" class="text-lg font-semibold text-highlighted">Revision history</h2>
          <p class="mt-1 text-sm text-toned">Each row identifies an exact, content-addressed revision.</p>
        </div>
        <span class="text-xs text-muted">{{ revisionHistory.length }} revision{{ revisionHistory.length === 1 ? '' : 's' }}</span>
      </div>
      <p v-if="revisionsError" class="mt-4 border-l-2 border-warning pl-3 text-sm text-toned" role="status" aria-live="polite">Revision history is unavailable. The selected revision remains available.</p>
      <ol v-else-if="showRevisionHistory" class="mt-4 divide-y divide-default border-y border-default" aria-label="Resource revision history">
        <li v-for="revision in revisionHistory" :key="revision.content_digest" class="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 px-2 py-3" :class="isSelectedRevision(revision) ? 'bg-[var(--am-color-bg-subtle)]' : ''">
          <NuxtLink v-if="revisionURL(revision)" :to="revisionURL(revision)" class="flex min-w-0 items-center gap-2 rounded focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary" :aria-current="isSelectedRevision(revision) ? 'page' : undefined">
            <UIcon v-if="isSelectedRevision(revision)" name="i-lucide-circle-check" class="size-4 shrink-0 text-primary" aria-hidden="true" />
            <span>
              <span class="block text-sm font-medium" :class="isSelectedRevision(revision) ? 'text-highlighted' : 'text-primary'">Scanned {{ formatDate(revision.assessment.assessed_at) }}</span>
              <span class="mt-0.5 flex items-center gap-2 text-xs text-muted">
                <span v-if="isCurrentRevision(revision)">Latest</span>
                <span :class="revisionStatusClass(revision)">{{ resourceAvailabilityCopy(revision, acquisition, kind).label }}</span>
              </span>
            </span>
          </NuxtLink>
          <div class="ml-auto flex items-center gap-2">
            <span class="font-mono text-xs text-muted" :title="revision.content_digest">{{ revisionLabel(revision) }}</span>
            <CopyButton :copied="copied === `revision-${revision.content_digest}`" :copy-label="`Copy revision ${revisionLabel(revision)}`" copied-label="Revision copied" icon-only @click="copy(revision.content_digest, `revision-${revision.content_digest}`)" />
          </div>
        </li>
      </ol>
    </section>

    <section v-else-if="activeTab === 'registry'" id="registry-panel" role="tabpanel" aria-labelledby="registry-tab" class="py-6">
      <RegistryOverview v-if="registryDetails" :registry="registryDetails" :operator="operator" />
      <p v-else class="text-sm text-muted">Registry details are unavailable.</p>
    </section>

    <section v-else id="technical-panel" role="tabpanel" aria-labelledby="technical-tab" class="py-6">
      <!-- Provenance reads as a record, so it is set as label/value pairs in mono
           rather than as prose. -->
      <h2 class="text-lg font-semibold text-highlighted">Technical details</h2>
      <dl class="mt-4 divide-y divide-default border-y border-default">
        <div v-for="entry in technicalEntries" :key="entry.label" class="grid gap-2 py-3 sm:grid-cols-[10rem_minmax(0,1fr)_auto] sm:items-start sm:gap-4">
          <dt class="text-sm font-medium text-muted">{{ entry.label }}</dt>
          <dd class="min-w-0 break-all font-mono text-sm text-highlighted" :title="entry.value">{{ entry.value }}</dd>
          <CopyButton v-if="entry.copyable" :copied="copied === `technical-${entry.label}`" :copy-label="`Copy ${entry.label}`" :copied-label="`${entry.label} copied`" icon-only @click="copy(entry.value, `technical-${entry.label}`)" />
        </div>
      </dl>
      <div v-if="imageFiles.length || binaryFiles.length" class="mt-6">
        <h3 class="text-base font-semibold text-highlighted">Additional files</h3>
        <div v-if="imageFiles.length" class="mt-4 grid gap-4 sm:grid-cols-2">
          <figure v-for="file in imageFiles" :key="file.path">
            <img :src="inspectionFileURL(file.content_digest)" :alt="`Contained preview ${file.path}`" class="max-h-64 max-w-full object-contain" loading="lazy" referrerpolicy="no-referrer">
            <figcaption class="mt-2 break-all font-mono text-xs text-muted">{{ file.path }} · {{ file.content_digest }}</figcaption>
          </figure>
        </div>
        <div v-if="binaryFiles.length" class="mt-4 text-sm text-muted">
          <p class="font-medium text-highlighted">Binary metadata (not rendered)</p>
          <ul class="mt-2 space-y-1 font-mono text-xs"><li v-for="file in binaryFiles" :key="file.path">{{ file.path }} · {{ file.media_type }} · {{ file.content_digest }} · {{ file.size }} bytes</li></ul>
        </div>
      </div>
    </section>
  </article>
</template>
