<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { AdminError, adminDiff, cloneAdmin, createAdminApi, lifecycleActions, previewIsStale, restoreRegistrySettings } from '~/lib/admin'
import type { AdminChange, AdminConfiguration, AdminPreviewState, AdminProfiles, AdminProposal } from '~/lib/admin'
import { destructiveLifecycleActions } from '~/lib/admin-presentation'

const props = defineProps<{ initial: AdminProposal, configuration: AdminConfiguration, profiles: AdminProfiles }>()
const emit = defineEmits<{ accepted: [change: AdminChange], cancel: [] }>()
const api = createAdminApi()
const draft = ref(cloneAdmin(props.initial))
const baseline = ref(cloneAdmin(props.configuration))
const stage = ref<'edit' | 'review' | 'observe'>('edit')
const busy = ref(false)
const error = ref<AdminError>()
const warnings = ref<string[]>([])
const accepted = ref<AdminChange>()
const reviewed = ref<AdminProposal>()
const acknowledgeUnknown = ref(false)
const previewState = ref<AdminPreviewState>()
// The review step reports on the preview only when it belongs to the draft
// under review; a stale or unfinished preview is an unknown, not evidence.
const previewSummary = computed(() => {
  const preview = previewState.value?.preview
  if (!preview || previewIsStale(previewState.value, draft.value.registry)) return { tone: 'warning' as const, text: preview ? 'The draft changed after its last discovery preview. Discovery for this exact draft has not been previewed.' : 'Discovery has not been previewed for this draft. Apply is still allowed, but a wrong root or unreachable origin will only surface when indexing runs.' }
  if (preview.status === 'failed') return { tone: 'error' as const, text: `The discovery preview for this draft failed (${preview.failure?.code}). Indexing would most likely fail the same way.` }
  if (preview.result) return { tone: preview.result.total_resources ? 'success' as const : 'warning' as const, text: `The discovery preview found ${preview.result.total_resources} ${preview.result.total_resources === 1 ? 'resource' : 'resources'} at commit ${preview.result.commit.slice(0, 12)}${preview.result.removed?.length ? `; ${preview.result.removed.length} currently published ${preview.result.removed.length === 1 ? 'resource' : 'resources'} would disappear` : ''}.` }
  return { tone: 'warning' as const, text: 'The discovery preview for this draft has not finished yet.' }
})
const heading = ref<HTMLElement>()
const errorSummary = ref<HTMLElement>()
const steps = [{ id: 'edit', label: 'Edit' }, { id: 'review', label: 'Review' }, { id: 'observe', label: 'Result' }] as const
const stepIndex = computed(() => steps.findIndex(step => step.id === stage.value))
const changed = computed(() => !accepted.value)
const originalRegistry = computed(() => baseline.value.snapshot.registries.find(value => value.id === props.initial.registry?.id))
const before = computed(() => draft.value.action === 'set_policy' ? baseline.value.snapshot.license_policy : draft.value.action === 'upsert_registry' ? originalRegistry.value : { lifecycle: baseline.value.snapshot.registries.find(value => value.id === draft.value.registry_id)?.lifecycle })
const after = computed(() => draft.value.license_policy || draft.value.registry || { action: draft.value.action })
const diff = computed(() => adminDiff(before.value, after.value))
const lifecycle = computed(() => lifecycleActions.find(value => value.action === draft.value.action))
const destructive = computed(() => destructiveLifecycleActions.has(draft.value.action))
const originMove = computed(() => !!originalRegistry.value && originalRegistry.value.origin !== draft.value.registry?.origin)
const unrestricted = computed(() => draft.value.action === 'set_policy' && !draft.value.license_policy?.allowlist?.length)
const title = computed(() => lifecycle.value?.label || (draft.value.action === 'set_policy' ? 'Edit license policy' : originalRegistry.value ? `Edit ${originalRegistry.value.slug}` : 'Add registry'))
const blocked = computed(() => error.value && ['unauthenticated', 'forbidden', 'revision_conflict', 'idempotency_conflict', 'network_error', 'invalid_response'].includes(error.value.code))
const errorTitle = computed(() => ({ unauthenticated: 'Your session expired', forbidden: 'Not authorized', revision_conflict: 'Configuration changed while you were editing', idempotency_conflict: 'This request was already submitted', network_error: 'The request outcome is unknown', invalid_response: 'Unexpected response', invalid_configuration: 'Please fix the highlighted problems' } as Record<string, string>)[error.value?.code || ''] || 'The request could not be completed')

watch(stage, async () => { await nextTick(); heading.value?.focus() })
watch(error, async (value) => { if (value) { await nextTick(); errorSummary.value?.focus() } })
function captureError(value: unknown) {
  error.value = value instanceof AdminError ? value : new AdminError('request_failed', 'The request could not be completed.')
  reviewed.value = undefined
  stage.value = 'edit'
}
async function validate() {
  if (busy.value || blocked.value) return
  error.value = undefined
  if (!draft.value.reason.trim()) { error.value = new AdminError('invalid_configuration', 'A reason is required.', { reason: 'Explain why this change is needed.' }); return }
  if (draft.value.registry && !props.initial.registry?.id && baseline.value.snapshot.registries.some(value => value.id === draft.value.registry?.id)) {
    error.value = new AdminError('invalid_configuration', 'That registry ID already exists. Open its configuration to edit it.', { id: 'Use a new immutable identity.' }); return
  }
  if (draft.value.registry?.license_override && !draft.value.registry.license_override.allowlist?.length) {
    error.value = new AdminError('invalid_configuration', 'An empty registry license override is ambiguous. Choose inheritance or a nonempty allowlist.'); return
  }
  busy.value = true
  try {
    const proposal = cloneAdmin(draft.value)
    const result = await api.validate(proposal, baseline.value.revision)
    if (result.valid !== true) throw new AdminError('invalid_configuration', 'Static validation did not succeed.')
    warnings.value = result.warnings || []
    reviewed.value = proposal
    acknowledgeUnknown.value = false
    stage.value = 'review'
  }
  catch (value) { captureError(value) }
  finally { busy.value = false }
}
async function apply() {
  if (!reviewed.value || !acknowledgeUnknown.value || busy.value || blocked.value) return
  busy.value = true
  try {
    accepted.value = await api.apply(reviewed.value, baseline.value.revision)
    stage.value = 'observe'
    emit('accepted', accepted.value)
  }
  catch (value) { captureError(value) }
  finally { busy.value = false }
}
async function reloadBaseline() {
  busy.value = true
  try {
    // Keep the draft, but never implicitly merge or resubmit it. The new diff
    // and a fresh validation/review are mandatory after an explicit reload.
    baseline.value = await api.configuration()
    const current = baseline.value.snapshot.registries.find(value => value.id === draft.value.registry?.id)
    if (current && draft.value.registry) {
      draft.value.registry = restoreRegistrySettings(current, draft.value.registry)
    }
    reviewed.value = undefined
    error.value = undefined
    stage.value = 'edit'
  }
  catch (value) { captureError(value) }
  finally { busy.value = false }
}
function canLeave() { return !changed.value || window.confirm('Discard this unsaved admin draft?') }
function cancel() { if (canLeave()) emit('cancel') }
function beforeUnload(event: BeforeUnloadEvent) { if (changed.value) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
onBeforeRouteLeave(canLeave)
onBeforeRouteUpdate(canLeave)
</script>

<template>
  <section class="overflow-hidden rounded-xl border border-default bg-[var(--am-color-bg-surface)]" aria-label="Change editor">
    <div class="flex flex-wrap items-center justify-between gap-4 border-b border-default bg-[var(--am-color-bg-subtle)] px-5 py-4 sm:px-8">
      <div class="flex items-center gap-3">
        <UIcon :name="destructive ? 'i-lucide-triangle-alert' : 'i-lucide-file-pen-line'" class="size-5" :class="destructive ? 'text-error' : 'text-primary'" aria-hidden="true" />
        <div><h2 ref="heading" tabindex="-1" class="text-lg font-semibold text-highlighted focus:outline-none">{{ title }}</h2><p class="text-xs text-muted">Based on revision {{ baseline.revision }}<span v-if="draft.registry_id"> · {{ draft.registry_id }}</span></p></div>
      </div>
      <ol class="flex items-center gap-2 text-xs" aria-label="Progress">
        <li v-for="(step, index) in steps" :key="step.id" class="flex items-center gap-2" :aria-current="step.id === stage ? 'step' : undefined">
          <span class="flex size-5 items-center justify-center rounded-full text-[11px] font-semibold" :class="index < stepIndex ? 'bg-primary text-inverted' : index === stepIndex ? 'bg-primary/15 text-primary ring-1 ring-primary' : 'bg-[var(--am-color-bg-surface)] text-muted ring-1 ring-default'"><UIcon v-if="index < stepIndex" name="i-lucide-check" class="size-3" aria-hidden="true" /><template v-else>{{ index + 1 }}</template></span>
          <span :class="index === stepIndex ? 'font-medium text-highlighted' : 'text-muted'">{{ step.label }}</span>
          <UIcon v-if="index < steps.length - 1" name="i-lucide-minus" class="size-3 text-dimmed" aria-hidden="true" />
        </li>
      </ol>
    </div>

    <div class="space-y-5 p-5 sm:p-8">
      <div v-if="error" ref="errorSummary" role="alert" tabindex="-1" class="space-y-2 rounded-lg border border-error bg-error/5 p-4 text-sm focus:outline-none">
        <p class="font-semibold text-error">{{ errorTitle }}</p>
        <p class="text-toned">{{ error.message }}</p>
        <ul v-if="Object.keys(error.fields).length" class="list-inside list-disc text-toned"><li v-for="(message, field) in error.fields" :key="field"><span class="font-mono text-xs">{{ field }}</span>: {{ message }}</li></ul>
        <p v-if="error.code === 'invalid_configuration' && Object.keys(error.fields).length" class="text-xs text-muted">The affected fields are outlined below. This is the same static validation that runs before review; nothing has been applied.</p>
        <p v-if="error.code === 'unauthenticated'" class="text-muted">Sign in again through your deployment, then reload this page. The draft stays in this tab and is never replayed.</p>
        <p v-if="error.code === 'network_error'" class="text-muted">Do not assume the change failed. Check Activity in another tab before starting a new request.</p>
        <div v-if="error.code === 'revision_conflict'" class="space-y-2"><p class="text-muted">Someone else changed the configuration. Load the latest revision and inspect the new diff before continuing.</p><UButton size="sm" :disabled="busy" icon="i-lucide-refresh-cw" @click="reloadBaseline">Load latest revision and keep my draft</UButton></div>
        <p v-if="error.code !== 'invalid_configuration'" class="text-xs text-muted">Error code <code class="font-mono">{{ error.code }}</code></p>
      </div>

      <template v-if="stage === 'edit'">
        <div v-if="lifecycle" class="flex gap-3 rounded-lg border p-4 text-sm" :class="destructive ? 'border-error/40 bg-error/5' : 'border-default bg-[var(--am-color-bg-subtle)]'"><UIcon name="i-lucide-info" class="mt-0.5 size-4 shrink-0" :class="destructive ? 'text-error' : 'text-muted'" aria-hidden="true" /><p class="text-toned">{{ lifecycle.description }}</p></div>
        <form class="space-y-6" @submit.prevent="validate">
          <fieldset :disabled="busy || !!blocked" class="space-y-6">
            <AdminRegistryFields v-if="draft.registry" v-model="draft.registry" :profiles="profiles" :existing="!!originalRegistry" :errors="error?.fields" />
            <AdminPreviewPanel v-if="draft.registry" v-model="previewState" :registry="draft.registry" :revision="baseline.revision" :disabled="busy || !!blocked" @rejected="captureError" />
            <AdminPolicyFields v-if="draft.license_policy" v-model="draft.license_policy" prefix="global" :error="error?.fields.license_policy" />
            <div v-if="originMove || unrestricted" class="space-y-2 rounded-lg border border-warning/40 bg-warning/5 p-4 text-sm">
              <label v-if="originMove" class="flex items-start gap-2"><input v-model="draft.confirm_origin_move" type="checkbox" required class="mt-1 size-4 accent-[var(--ui-primary)]"><span>I confirm the origin move for the same immutable registry identity.</span></label>
              <label v-if="unrestricted" class="flex items-start gap-2"><input v-model="draft.confirm_unrestricted" type="checkbox" required class="mt-1 size-4 accent-[var(--ui-primary)]"><span>I understand that an empty global allowlist disables license enforcement.</span></label>
            </div>
            <AdminField id="change-reason" label="Reason for change" required description="Recorded in the activity log with your identity." :error="error?.fields.reason">
              <UTextarea id="change-reason" v-model="draft.reason" required :maxlength="1024" :rows="3" autoresize placeholder="Why is this change needed?" class="w-full" :aria-invalid="!!error?.fields.reason" :aria-describedby="error?.fields.reason ? 'change-reason-help' : undefined" />
            </AdminField>
            <div class="flex flex-wrap items-center gap-3 border-t border-default pt-5">
              <UButton type="submit" :loading="busy" trailing-icon="i-lucide-arrow-right">Continue to review</UButton>
              <UButton color="neutral" variant="ghost" :disabled="busy" @click="cancel">Discard draft</UButton>
              <p class="ml-auto text-xs text-muted">Nothing is applied until you confirm on the next step.</p>
            </div>
          </fieldset>
        </form>
      </template>

      <template v-else-if="stage === 'review'">
        <div class="space-y-1"><h3 class="text-base font-semibold text-highlighted">Review changes</h3><p class="text-sm text-toned">Static validation passed. No repository has been contacted yet.</p></div>
        <div v-if="diff.length" class="overflow-hidden rounded-lg border border-default">
          <table class="w-full text-left text-sm"><caption class="sr-only">Configuration diff against revision {{ baseline.revision }}</caption><thead class="bg-[var(--am-color-bg-subtle)] text-xs uppercase tracking-wide text-muted"><tr><th class="px-3 py-2 font-semibold">Field</th><th class="px-3 py-2 font-semibold">Current</th><th class="px-3 py-2 font-semibold">Proposed</th></tr></thead><tbody class="divide-y divide-default"><tr v-for="row in diff" :key="row.field"><th scope="row" class="px-3 py-2 align-top font-mono text-xs font-medium text-highlighted">{{ row.field }}</th><td class="max-w-xs break-words px-3 py-2 align-top"><code class="rounded bg-error/10 px-1 font-mono text-xs text-error line-through decoration-error/50">{{ row.before }}</code></td><td class="max-w-xs break-words px-3 py-2 align-top"><code class="rounded bg-success/10 px-1 font-mono text-xs text-success">{{ row.after }}</code></td></tr></tbody></table>
        </div>
        <p v-else class="rounded-lg border border-dashed border-default p-4 text-sm text-muted">No configuration fields change. This request still records an audited operational action.</p>
        <p class="text-sm"><span class="text-muted">Reason:</span> <span class="text-toned">{{ reviewed?.reason }}</span></p>
        <ul v-if="warnings.length" class="space-y-1 rounded-lg border border-warning/40 bg-warning/5 p-4 text-sm"><li v-for="warning in warnings" :key="warning" class="flex gap-2"><UIcon name="i-lucide-triangle-alert" class="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />{{ warning }}</li></ul>
        <div class="space-y-3 rounded-lg border border-default bg-[var(--am-color-bg-subtle)] p-4 text-sm">
          <h3 class="font-semibold text-highlighted">Before you apply</h3>
          <p class="text-toned">{{ lifecycle?.description || 'Existing publication is retained unless current policy or withdrawal blocks it. New registries have no resources until publication succeeds.' }}</p>
          <p v-if="draft.registry" role="status" class="flex items-start gap-2 rounded-md border px-3 py-2" :class="{ 'border-success/40 bg-success/5': previewSummary.tone === 'success', 'border-warning/40 bg-warning/5': previewSummary.tone === 'warning', 'border-error/40 bg-error/5': previewSummary.tone === 'error' }"><UIcon :name="previewSummary.tone === 'success' ? 'i-lucide-telescope' : 'i-lucide-triangle-alert'" class="mt-0.5 size-4 shrink-0" :class="{ 'text-success': previewSummary.tone === 'success', 'text-warning': previewSummary.tone === 'warning', 'text-error': previewSummary.tone === 'error' }" aria-hidden="true" /><span class="text-toned">{{ previewSummary.text }}</span></p>
          <AdminHint summary="What cannot be previewed"><p>Retained-revision and license impact analysis are not available; this is not an exhaustive impact report.</p><p>A discovery preview observes one commit. A mutable ref can move before indexing; use an exact commit in the ref field if that matters.</p></AdminHint>
          <label class="flex items-start gap-2 font-medium text-highlighted"><input v-model="acknowledgeUnknown" type="checkbox" class="mt-1 size-4 accent-[var(--ui-primary)]"><span>I have reviewed the diff and understand these availability impacts and unknowns.</span></label>
        </div>
        <div class="flex flex-wrap items-center gap-3 border-t border-default pt-5">
          <UButton color="neutral" variant="outline" icon="i-lucide-arrow-left" :disabled="busy" @click="stage = 'edit'; reviewed = undefined">Back</UButton>
          <UButton :color="destructive ? 'error' : 'primary'" :disabled="!acknowledgeUnknown || busy" :loading="busy" icon="i-lucide-check" @click="apply">Apply change</UButton>
          <UButton color="neutral" variant="ghost" :disabled="busy" class="ml-auto" @click="cancel">Discard draft</UButton>
        </div>
      </template>

      <template v-else-if="accepted">
        <div class="flex items-start gap-3 rounded-lg border border-success/40 bg-success/5 p-4">
          <UIcon name="i-lucide-circle-check" class="mt-0.5 size-5 shrink-0 text-success" aria-hidden="true" />
          <div class="space-y-1 text-sm"><p role="status" class="font-semibold text-highlighted">Change accepted as revision {{ accepted.revision }}</p><p class="flex flex-wrap items-center gap-1 text-toned">Indexing work has been queued. Acceptance does not mean indexing succeeded.<AdminHint text="Watch the job status below or the registry's Indexing history for the actual outcome." /></p><AdminIdChip label="Request" :value="accepted.request_id" /></div>
        </div>
        <AdminJobs :ids="accepted.job_ids || []" observe />
        <div class="border-t border-default pt-5"><UButton color="neutral" variant="outline" @click="cancel">Close</UButton></div>
      </template>
    </div>
  </section>
</template>
