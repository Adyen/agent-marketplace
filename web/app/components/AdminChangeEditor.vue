<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { AdminError, adminDiff, cloneAdmin, createAdminApi, lifecycleActions, restoreRegistrySettings } from '~/lib/admin'
import type { AdminChange, AdminConfiguration, AdminProfiles, AdminProposal } from '~/lib/admin'

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
const heading = ref<HTMLElement>()
const errorSummary = ref<HTMLElement>()
const changed = computed(() => !accepted.value)
const originalRegistry = computed(() => baseline.value.snapshot.registries.find(value => value.id === props.initial.registry?.id))
const before = computed(() => draft.value.action === 'set_policy' ? baseline.value.snapshot.license_policy : draft.value.action === 'upsert_registry' ? originalRegistry.value : { lifecycle: baseline.value.snapshot.registries.find(value => value.id === draft.value.registry_id)?.lifecycle })
const after = computed(() => draft.value.license_policy || draft.value.registry || { action: draft.value.action })
const diff = computed(() => adminDiff(before.value, after.value))
const lifecycle = computed(() => lifecycleActions.find(value => value.action === draft.value.action))
const originMove = computed(() => !!originalRegistry.value && originalRegistry.value.origin !== draft.value.registry?.origin)
const unrestricted = computed(() => draft.value.action === 'set_policy' && !draft.value.license_policy?.allowlist?.length)
const title = computed(() => lifecycle.value?.label || (draft.value.action === 'set_policy' ? 'Edit license policy' : 'Edit registry configuration'))
const blocked = computed(() => error.value && ['unauthenticated', 'forbidden', 'revision_conflict', 'idempotency_conflict', 'network_error', 'invalid_response'].includes(error.value.code))

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
  <section class="space-y-5 rounded-xl border border-default bg-default p-5 sm:p-8" aria-label="Change editor">
    <div class="flex flex-wrap items-center justify-between gap-3"><h2 ref="heading" tabindex="-1" class="text-xl font-semibold">{{ title }}</h2><span class="text-sm text-muted">{{ stage === 'edit' ? '1. Edit and validate' : stage === 'review' ? '2. Review and apply' : '3. Observe' }} · based on r{{ baseline.revision }}</span></div>
    <div v-if="error" ref="errorSummary" role="alert" tabindex="-1" class="space-y-2 rounded border border-error p-4">
      <p class="font-medium">{{ error.code }}</p><p>{{ error.message }}</p>
      <ul v-if="Object.keys(error.fields).length" class="list-inside list-disc"><li v-for="(message, field) in error.fields" :key="field">{{ field }}: {{ message }}</li></ul>
      <p v-if="error.code === 'unauthenticated'">Your SSO session expired. Reauthenticate through your deployment, then reload this page. The draft is held only in this page and will not be replayed.</p>
      <p v-if="error.code === 'network_error'">Do not assume the change failed. Check Activity in another tab before starting a new request.</p>
      <template v-if="error.code === 'revision_conflict'"><p>Someone changed the configuration. Load the latest revision and inspect the new diff before validating again.</p><UButton :disabled="busy" @click="reloadBaseline">Load latest revision; keep draft for review</UButton></template>
    </div>
    <template v-if="stage === 'edit'">
      <p v-if="lifecycle" class="rounded bg-muted p-4">{{ lifecycle.description }} Scope: {{ draft.registry_id }}.</p>
      <form class="space-y-5" @submit.prevent="validate">
        <fieldset :disabled="busy || !!blocked" class="space-y-5">
          <AdminRegistryFields v-if="draft.registry" v-model="draft.registry" :profiles="profiles" :existing="!!originalRegistry" />
          <AdminPolicyFields v-if="draft.license_policy" v-model="draft.license_policy" prefix="global" />
          <label v-if="originMove" class="flex items-start gap-2"><input v-model="draft.confirm_origin_move" type="checkbox" required>I confirm the origin move for the same immutable registry identity.</label>
          <label v-if="unrestricted" class="flex items-start gap-2"><input v-model="draft.confirm_unrestricted" type="checkbox" required>I understand that an empty global allowlist disables license enforcement.</label>
          <div><label for="change-reason" class="block font-medium">Reason for change</label><textarea id="change-reason" v-model="draft.reason" required maxlength="1024" :aria-invalid="!!error?.fields.reason" :aria-describedby="error?.fields.reason ? 'reason-error' : undefined" rows="3" class="mt-1 w-full rounded border border-default bg-default p-3" /><p v-if="error?.fields.reason" id="reason-error" class="text-error">{{ error.fields.reason }}</p></div>
          <UButton type="submit" :loading="busy">Validate configuration</UButton>
        </fieldset>
      </form>
    </template>
    <template v-else-if="stage === 'review'">
      <h3 class="font-semibold">Review changes</h3>
      <p>Static validation passed. No repository acquisition has been performed.</p>
      <div class="overflow-x-auto"><table class="w-full text-left text-sm"><caption class="sr-only">Configuration diff against revision {{ baseline.revision }}</caption><thead><tr><th class="p-2">Field</th><th class="p-2">Current</th><th class="p-2">Proposed</th></tr></thead><tbody><tr v-for="row in diff" :key="row.field" class="border-t border-default"><th class="p-2 align-top">{{ row.field }}</th><td class="max-w-xs break-words p-2 align-top"><code>{{ row.before }}</code></td><td class="max-w-xs break-words p-2 align-top"><code>{{ row.after }}</code></td></tr></tbody></table></div>
      <p v-if="!diff.length">No configuration field differences. This request may still create an audited operational action.</p>
      <p><strong>Reason:</strong> {{ reviewed?.reason }}</p>
      <ul v-if="warnings.length" class="list-inside list-disc"><li v-for="warning in warnings" :key="warning">{{ warning }}</li></ul>
      <h3 class="font-semibold">Availability impact and unknowns</h3>
      <p>{{ lifecycle?.description || 'Existing publication is retained unless current policy or withdrawal blocks it. New registries have no resources until publication succeeds.' }}</p>
      <p>Resource impact counts and retained-revision impact analysis are not available. This is not an exhaustive impact report.</p>
      <p>Network preview is unavailable. A mutable ref can move before indexing; use an exact commit in the ref field if required.</p>
      <label class="flex items-start gap-2"><input v-model="acknowledgeUnknown" type="checkbox">I have reviewed the diff and understand these availability impacts and unknowns.</label>
      <div class="flex gap-3"><UButton color="neutral" variant="outline" :disabled="busy" @click="stage = 'edit'; reviewed = undefined">Back to edit</UButton><UButton :disabled="!acknowledgeUnknown || busy" :loading="busy" @click="apply">Apply reviewed change</UButton></div>
    </template>
    <template v-else-if="accepted">
      <p role="status" class="font-medium">Request accepted at revision {{ accepted.revision }}. This does not mean indexing succeeded.</p>
      <p>Request ID: {{ accepted.request_id }}</p>
      <AdminJobs :ids="accepted.job_ids || []" observe />
    </template>
    <UButton color="neutral" variant="outline" :disabled="busy" @click="cancel">{{ accepted ? 'Close' : 'Discard draft' }}</UButton>
  </section>
</template>
