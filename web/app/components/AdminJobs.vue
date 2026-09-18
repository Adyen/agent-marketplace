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
  <div class="space-y-3">
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="!ids.length">No resulting job IDs were returned.</p>
    <ul class="space-y-2"><li v-for="job in jobs" :key="job.id" class="rounded border border-default p-3"><p class="break-all font-medium">{{ job.kind }} · {{ job.status }} · {{ job.id }}</p><p class="text-sm text-muted">Attempt {{ job.attempt }} / {{ job.max_attempts }} · Updated {{ job.updated_at }}</p></li></ul>
    <UButton v-if="ids.length" color="neutral" variant="outline" :loading="busy" @click="refresh">Refresh observed job status</UButton>
  </div>
</template>
