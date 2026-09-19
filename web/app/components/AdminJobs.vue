<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { createAdminApi } from '~/lib/admin'
import type { AdminJob } from '~/lib/admin'
const props = defineProps<{ ids: string[], observe?: boolean }>()
const jobs = ref<AdminJob[]>([])
const error = ref('')
const busy = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
let remaining = 12
let disposed = false
async function refresh() {
  if (busy.value || disposed) return
  clearTimeout(timer)
  busy.value = true
  error.value = ''
  try {
    jobs.value = await Promise.all(props.ids.map(id => createAdminApi().job(id)))
    if (!disposed && props.observe && remaining-- > 0 && jobs.value.some(job => ['pending', 'leased', 'queued', 'running'].includes(job.status))) {
      timer = setTimeout(() => { if (document.visibilityState === 'visible') void refresh() }, 5000)
    }
  }
  catch { error.value = 'Job status could not be loaded. Automatic observation stopped. No worker health or publication outcome can be inferred.' }
  finally { busy.value = false }
}
onMounted(refresh)
onBeforeUnmount(() => { disposed = true; clearTimeout(timer) })
</script>

<template>
  <div class="space-y-2">
    <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
    <p v-if="!ids.length" class="text-xs text-muted">No jobs were queued by this request.</p>
    <ul v-else class="space-y-1.5">
      <li v-for="job in jobs" :key="job.id" class="flex flex-wrap items-center gap-2 rounded-lg border border-default bg-[var(--am-color-bg-subtle)] px-3 py-2 text-sm">
        <AdminStatusBadge :value="job.status" kind="outcome" />
        <span class="font-medium text-highlighted">{{ job.kind }}</span>
        <span class="text-xs text-muted">attempt {{ job.attempt }} of {{ job.max_attempts }} · updated <AdminTime :value="job.updated_at" /></span>
        <AdminIdChip label="Job" :value="job.id" class="ml-auto" />
      </li>
    </ul>
    <UButton v-if="ids.length" color="neutral" variant="ghost" size="xs" icon="i-lucide-refresh-cw" :loading="busy" @click="refresh">Refresh job status</UButton>
  </div>
</template>
