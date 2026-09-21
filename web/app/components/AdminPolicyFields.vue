<script setup lang="ts">
import { ref } from 'vue'
import type { AdminPolicy } from '~/lib/admin'
const policy = defineModel<AdminPolicy>({ required: true })
defineProps<{ prefix: string, override?: boolean, error?: string }>()
const control = 'w-full rounded-md border border-default bg-default px-2.5 py-1.5 text-sm text-highlighted focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-primary disabled:opacity-60'
// Local text keeps trailing newlines and separators while typing; the model
// only ever holds the normalised list.
const text = ref((policy.value.allowlist || []).join('\n'))
function setLicenses(value: string | number) {
  text.value = String(value)
  policy.value.allowlist = text.value.split(/[\n,]/).map(item => item.trim()).filter(Boolean)
}
</script>

<template>
  <div class="grid gap-4 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
    <AdminField :id="`${prefix}-allowlist`" label="License allowlist" :description="override ? 'One SPDX license per line. A nonempty override replaces the inherited allowlist; empty overrides are rejected.' : 'One SPDX license per line. Leave empty to disable license enforcement.'" :error="error">
      <UTextarea :id="`${prefix}-allowlist`" :model-value="text" :rows="4" placeholder="MIT&#10;Apache-2.0" class="w-full font-mono" :aria-invalid="error ? 'true' : undefined" :aria-describedby="`${prefix}-allowlist-help`" @update:model-value="setLicenses" />
    </AdminField>
    <AdminField :id="`${prefix}-undeclared`" label="Undeclared license" description="What happens when a resource declares no license.">
      <select :id="`${prefix}-undeclared`" v-model="policy.undeclared" :class="control">
        <option v-if="override" value="">Inherit</option>
        <option value="reject">Reject</option><option value="warn">Warn</option><option value="allow">Allow</option>
      </select>
    </AdminField>
  </div>
</template>
