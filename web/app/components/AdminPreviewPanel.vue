<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { AdminError, createAdminApi, previewIsStale, registryDraftKey } from '~/lib/admin'
import type { AdminPreviewState, AdminRegistry } from '~/lib/admin'
import { changeBadge, decisionBadge, previewFailureTitle, previewKindCounts } from '~/lib/admin-presentation'
import { KIND_ICONS, KIND_LABELS } from '~/lib/presentation'
import type { ResourceKind } from '~/types/marketplace'

// Asks the worker to acquire the drafted registry and report what discovery
// would find, before anything is applied. Nothing here publishes; the result
// is bound to the exact draft it was requested for.
const props = defineProps<{ registry: AdminRegistry, revision: number, disabled?: boolean }>()
const state = defineModel<AdminPreviewState>()
const api = createAdminApi()
const busy = ref(false)
const error = ref('')
const waited = ref(0)
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false

const preview = computed(() => state.value?.preview)
const result = computed(() => preview.value?.result)
const stale = computed(() => previewIsStale(state.value, props.registry))
const active = computed(() => preview.value?.status === 'pending' || preview.value?.status === 'running')
const failure = computed(() => preview.value?.failure ? previewFailureTitle(preview.value.failure.code) : undefined)
const emptyRoots = computed(() => result.value?.roots.filter(root => root.resource_count === 0) ?? [])
const kindCounts = computed(() => previewKindCounts(result.value?.roots.flatMap(root => root.resources) ?? []))
const changeSummary = computed(() => {
  if (!result.value?.diff_base) return undefined
  const counts = { added: 0, changed: 0, unchanged: 0 }
  for (const item of result.value.roots.flatMap(root => root.resources)) if (item.change && item.change in counts) counts[item.change as keyof typeof counts]++
  return { ...counts, removed: result.value.removed?.length ?? 0 }
})
const waitingHint = computed(() => waited.value >= 15 ? 'No worker has picked this up yet. In API-only deployments previews stay queued until a worker process runs.' : 'Cloning the repository and running discovery. This usually takes a few seconds.')

function kindLabelOf(kind: string) { return KIND_LABELS[kind as ResourceKind] ?? kind }
function kindIconOf(kind: string) { return KIND_ICONS[kind as ResourceKind] ?? 'i-lucide-file' }
function stop() { clearTimeout(timer); timer = undefined }
function schedule() {
  stop()
  if (disposed || !active.value) return
  timer = setTimeout(() => { waited.value += 3; if (document.visibilityState === 'visible') void poll(); else schedule() }, 3000)
}
async function poll() {
  const id = preview.value?.id
  if (!id || !state.value) return
  try {
    const next = await api.previewStatus(id)
    if (!disposed && state.value?.preview.id === id) state.value = { ...state.value, preview: next }
  }
  catch (value) { error.value = describe(value, 'The preview status could not be loaded.') }
  schedule()
}
async function request() {
  if (busy.value || props.disabled) return
  busy.value = true
  error.value = ''
  waited.value = 0
  try {
    const created = await api.preview({ action: 'upsert_registry', registry: props.registry, registry_id: props.registry.id, reason: '' }, props.revision)
    state.value = { preview: created, registry: registryDraftKey(props.registry) }
  }
  catch (value) { error.value = describe(value, 'The preview could not be requested.') }
  finally { busy.value = false }
}
function describe(value: unknown, fallback: string) {
  if (!(value instanceof AdminError)) return fallback
  if (value.code === 'rate_limited') return 'Preview limit reached for now. Wait a minute before requesting another one.'
  if (value.code === 'invalid_configuration') return 'Fix the highlighted configuration problems first; the preview runs the same static validation as Continue to review.'
  if (value.code === 'revision_conflict') return 'The configuration changed since this draft was opened. Continue to review to load the latest revision.'
  return `${value.code}: ${value.message}`
}
watch(active, value => value ? schedule() : stop(), { immediate: true })
onBeforeUnmount(() => { disposed = true; stop() })
</script>

<template>
  <section class="overflow-hidden rounded-lg border border-default" aria-labelledby="preview-heading">
    <div class="flex flex-wrap items-center justify-between gap-3 bg-[var(--am-color-bg-subtle)] px-4 py-3">
      <div class="flex min-w-0 items-center gap-2">
        <UIcon name="i-lucide-telescope" class="size-4 shrink-0 text-primary" aria-hidden="true" />
        <h3 id="preview-heading" class="text-sm font-semibold text-highlighted">Discovery preview</h3>
        <AdminHint text="Clones the origin at the given ref on the worker and reports what indexing would discover under each root. Nothing is published, and a mutable ref can still move before indexing." />
      </div>
      <div class="flex items-center gap-2">
        <span v-if="preview && !active" class="text-xs text-muted">Observed <AdminTime :value="result?.observed_at || preview.completed_at" /></span>
        <UButton type="button" size="sm" :color="preview ? 'neutral' : 'primary'" :variant="preview ? 'outline' : 'solid'" icon="i-lucide-radar" :loading="busy" :disabled="disabled || active" @click="request">{{ preview ? 'Preview again' : 'Preview discovery' }}</UButton>
      </div>
    </div>

    <div class="space-y-4 p-4">
      <p v-if="error" role="alert" class="rounded-md border border-error/40 bg-error/5 px-3 py-2 text-sm text-error">{{ error }}</p>

      <p v-if="!preview" class="text-sm text-toned">See which skills, commands, subagents, hooks and MCP servers each source root exposes before you apply the change. A root that points at the wrong directory shows up here as an error instead of as an empty registry later.</p>

      <div v-else-if="stale" role="status" class="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-sm">
        <UIcon name="i-lucide-history" class="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
        <p class="text-toned">The draft changed since this preview was taken. The result below reflects the earlier draft; preview again to refresh it.</p>
      </div>

      <div v-if="preview && active" role="status" aria-busy="true" class="flex items-center gap-3 rounded-md border border-dashed border-default px-3 py-3 text-sm">
        <UIcon name="i-lucide-loader-circle" class="size-4 shrink-0 animate-spin text-primary" aria-hidden="true" />
        <div class="min-w-0 space-y-0.5"><p class="font-medium text-highlighted">{{ preview.status === 'running' ? 'Discovering resources…' : 'Waiting for a worker…' }}</p><p class="text-xs text-muted">{{ waitingHint }}</p></div>
        <AdminIdChip label="Preview" :value="preview.id" class="ml-auto" />
      </div>

      <div v-else-if="preview?.failure && failure" role="alert" class="space-y-2 rounded-md border border-error/40 bg-error/5 p-4 text-sm">
        <p class="flex items-center gap-2 font-semibold text-error"><UIcon name="i-lucide-circle-x" class="size-4" aria-hidden="true" />{{ failure.title }}</p>
        <p class="break-words font-mono text-xs text-highlighted">{{ preview.failure.message }}</p>
        <p class="text-toned">{{ failure.hint }}</p>
        <p class="text-xs text-muted">Indexing would fail the same way if you applied this draft. Error code <code class="font-mono">{{ preview.failure.code }}</code>.</p>
      </div>

      <template v-else-if="result">
        <div v-if="result.diagnostics?.some(item => item.code.startsWith('ingest.') && item.code.includes('.load-') && item.code.endsWith('-failed'))" role="status" class="rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-sm text-toned">
          <p class="font-semibold text-warning">Preview completed with warnings</p>
          <p>Malformed resources were skipped. Indexing will publish the valid resources below, without retaining older versions of skipped resources. See the catalog diagnostics for paths and details.</p>
        </div>
        <div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
          <p class="flex items-baseline gap-1.5"><span class="text-2xl font-semibold tabular-nums tracking-tight text-highlighted">{{ result.total_resources }}</span><span class="text-muted">{{ result.total_resources === 1 ? 'resource' : 'resources' }} at</span><code class="rounded bg-[var(--am-color-bg-subtle)] px-1.5 py-0.5 font-mono text-xs text-highlighted" :title="result.commit">{{ result.commit.slice(0, 12) }}</code><span class="text-muted">on</span><code class="font-mono text-xs text-highlighted">{{ result.ref || 'default branch' }}</code></p>
          <ul v-if="kindCounts.length" class="flex flex-wrap gap-1.5" aria-label="Resources by kind">
            <li v-for="[kind, count] in kindCounts" :key="kind" class="inline-flex items-center gap-1 rounded-full border border-default px-2 py-0.5 text-xs text-toned"><UIcon :name="kindIconOf(kind)" class="size-3" aria-hidden="true" />{{ count }} {{ kindLabelOf(kind) }}</li>
          </ul>
        </div>

        <div v-if="changeSummary" class="flex flex-wrap items-center gap-2 text-xs" aria-label="Changes against the current publication">
          <span class="text-muted">Against the current publication:</span>
          <UBadge v-if="changeSummary.added" color="success" variant="subtle" size="sm">{{ changeSummary.added }} new</UBadge>
          <UBadge v-if="changeSummary.changed" color="info" variant="subtle" size="sm">{{ changeSummary.changed }} changed</UBadge>
          <UBadge v-if="changeSummary.removed" color="error" variant="subtle" size="sm">{{ changeSummary.removed }} removed</UBadge>
          <UBadge v-if="changeSummary.unchanged" color="neutral" variant="subtle" size="sm">{{ changeSummary.unchanged }} unchanged</UBadge>
          <span v-if="!changeSummary.added && !changeSummary.changed && !changeSummary.removed" class="text-muted">no resource changes</span>
        </div>

        <div v-if="emptyRoots.length" role="status" class="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-sm">
          <UIcon name="i-lucide-folder-x" class="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
          <p class="text-toned">{{ emptyRoots.length === 1 ? 'One root' : `${emptyRoots.length} roots` }} exposed no resources: <code v-for="root in emptyRoots" :key="root.path" class="mr-1 font-mono text-xs text-highlighted">{{ root.path }}</code>. Applying as-is would publish nothing from {{ emptyRoots.length === 1 ? 'it' : 'them' }}.</p>
        </div>

        <ol class="space-y-3">
          <li v-for="(root, index) in result.roots" :key="index" class="overflow-hidden rounded-md border border-default">
            <div class="flex flex-wrap items-center gap-2 border-b border-default bg-[var(--am-color-bg-subtle)] px-3 py-2 text-sm">
              <span class="text-xs text-muted">{{ index + 1 }}.</span>
              <code class="font-mono text-highlighted">{{ root.path }}</code>
              <span class="text-xs text-muted">{{ root.source_harness || 'Autodetect' }}</span>
              <span class="ml-auto text-xs tabular-nums" :class="root.resource_count ? 'text-toned' : 'font-medium text-warning'">{{ root.resource_count }} {{ root.resource_count === 1 ? 'resource' : 'resources' }}<template v-if="root.truncated">, first {{ root.resources.length }} shown</template></span>
            </div>
            <ul v-if="root.resources.length" class="divide-y divide-default">
              <li v-for="item in root.resources" :key="`${item.kind}/${item.name}`" class="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 px-3 py-2 text-sm">
                <UIcon :name="kindIconOf(item.kind)" class="size-4 text-muted" :title="kindLabelOf(item.kind)" aria-hidden="true" />
                <div class="min-w-0">
                  <p class="flex flex-wrap items-baseline gap-x-2"><span class="font-medium text-highlighted">{{ item.name }}</span><span class="text-xs text-muted">{{ kindLabelOf(item.kind) }}</span><span v-if="item.description" class="truncate text-xs text-toned">{{ item.description }}</span></p>
                  <p v-if="item.source_path" class="truncate font-mono text-[11px] text-muted" :title="item.source_path">{{ item.source_path }}</p>
                </div>
                <div class="flex items-center gap-1.5">
                  <UBadge v-if="item.change && item.change !== 'unchanged'" :color="changeBadge(item.change).color" variant="subtle" size="sm">{{ changeBadge(item.change).label }}</UBadge>
                  <UBadge v-if="item.decision && item.decision !== 'allow'" :color="decisionBadge(item.decision).color" variant="subtle" size="sm" :title="item.findings ? `${item.findings} finding${item.findings === 1 ? '' : 's'}` : undefined">{{ decisionBadge(item.decision).label }}</UBadge>
                </div>
              </li>
            </ul>
            <p v-else class="px-3 py-3 text-sm text-muted">Nothing discoverable under this root. Check that it points at the directory containing <code class="font-mono text-xs">skills/</code>, <code class="font-mono text-xs">commands/</code> or a plugin manifest.</p>
          </li>
        </ol>

        <div v-if="result.removed?.length" class="overflow-hidden rounded-md border border-error/40">
          <p class="border-b border-error/40 bg-error/5 px-3 py-2 text-sm font-medium text-error">Currently published but no longer discovered</p>
          <ul class="divide-y divide-default">
            <li v-for="item in result.removed" :key="`${item.kind}/${item.name}`" class="flex flex-wrap items-baseline gap-x-2 px-3 py-2 text-sm"><span class="font-medium text-highlighted line-through decoration-error/60">{{ item.name }}</span><span class="text-xs text-muted">{{ kindLabelOf(item.kind) }}</span><span v-if="item.source_path" class="ml-auto font-mono text-[11px] text-muted">{{ item.source_path }}</span></li>
          </ul>
        </div>

        <AdminHint v-if="result.diagnostics?.length" :summary="`${result.diagnostics.length} catalog ${result.diagnostics.length === 1 ? 'diagnostic' : 'diagnostics'}`">
          <ul class="space-y-1">
            <li v-for="(item, index) in result.diagnostics" :key="index" class="flex flex-wrap gap-x-2 text-xs"><code class="font-mono text-highlighted">{{ item.root }}</code><code class="font-mono" :class="item.error ? 'text-error' : 'text-muted'">{{ item.code }}</code><span class="text-toned">{{ item.message }}</span></li>
          </ul>
        </AdminHint>

        <p class="flex flex-wrap items-center gap-x-2 text-xs text-muted"><AdminIdChip label="Preview" :value="preview!.id" /> · policy revision {{ result.policy_revision }} · the commit above is what the ref resolved to at request time; a mutable ref can move before indexing.</p>
      </template>
    </div>
  </section>
</template>
