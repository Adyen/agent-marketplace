<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { createAdminApi } from '~/lib/admin'
import type { AdminAttempt } from '~/lib/admin'

const props = defineProps<{ registryId: string }>()
const attempts = ref<AdminAttempt[]>([])
const nextBefore = ref(0)
const busy = ref(false)
const loaded = ref(false)
const error = ref('')
let disposed = false
async function load(older = false) {
  if (busy.value || disposed) return
  busy.value = true
  error.value = ''
  try {
    const result = await createAdminApi().attempts(props.registryId, older ? nextBefore.value : undefined)
    if (disposed) return
    attempts.value = older ? [...attempts.value, ...result.attempts] : result.attempts
    nextBefore.value = result.next_before
    loaded.value = true
  }
  catch { if (!disposed) error.value = 'Indexing attempts could not be loaded. Displayed events may be stale; retry to refresh. No indexing outcome can be inferred from this error.' }
  finally { busy.value = false }
}
onMounted(() => load())
onBeforeUnmount(() => { disposed = true })
</script>

<template>
  <section aria-label="Registry indexing attempts" class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-1.5"><h3 class="text-base font-semibold text-highlighted">Indexing attempts</h3><AdminHint text="Recorded events for this registry, newest first. A completed configuration job only queues this work; it does not mean these attempts succeeded. Started events are past observations, not current worker health." /></div>
      <UButton color="neutral" variant="outline" size="sm" icon="i-lucide-refresh-cw" :disabled="busy" :loading="busy" @click="load()">Refresh indexing attempts</UButton>
    </div>
    <p v-if="busy && !attempts.length" role="status" class="text-sm text-muted">Loading indexing attempts…</p>
    <p v-if="error" role="alert" class="rounded-lg border border-error px-4 py-3 text-sm text-error">{{ error }}</p>
    <ol v-if="attempts.length" class="divide-y divide-default overflow-hidden rounded-xl border border-default bg-[var(--am-color-bg-surface)]">
      <li v-for="attempt in attempts" :key="attempt.sequence">
        <article class="grid gap-2 px-4 py-3 text-sm sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center sm:gap-4">
          <AdminStatusBadge :value="attempt.outcome" kind="outcome" />
          <div class="min-w-0 space-y-1">
            <h4 class="font-medium text-highlighted">{{ attempt.operation }} <span class="font-normal text-muted">· attempt {{ attempt.attempt }}</span></h4>
            <p class="text-xs text-muted">Configuration r{{ attempt.configuration_revision }} · generation {{ attempt.registry_generation }} · policy r{{ attempt.policy_revision }}</p>
            <div class="flex flex-wrap gap-1.5"><AdminIdChip label="Job" :value="attempt.job_id" /><AdminIdChip label="Parent" :value="attempt.parent_job_id" /><AdminIdChip label="Publication" :value="attempt.publication_id" /></div>
            <p v-if="attempt.outcome === 'failed'" class="flex items-center gap-1 text-xs text-muted">Indexing failed.<AdminHint text="Detailed failure diagnostics are not returned by this endpoint yet. Check the worker logs for this job ID." /></p>
          </div>
          <AdminTime :value="attempt.recorded_at" class="text-xs text-muted" />
        </article>
      </li>
    </ol>
    <p v-if="loaded && !busy && !error && !attempts.length" class="rounded-xl border border-dashed border-default p-6 text-center text-sm text-muted">No indexing attempts have been recorded for this registry.</p>
    <UButton v-if="nextBefore" color="neutral" variant="outline" size="sm" :disabled="busy" @click="load(true)">Load older attempts</UButton>
  </section>
</template>
