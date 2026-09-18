<script setup lang="ts">
import type { AdminPolicy } from '~/lib/admin'
const policy = defineModel<AdminPolicy>({ required: true })
defineProps<{ prefix: string, override?: boolean }>()
function setLicenses(event: Event) {
  policy.value.allowlist = (event.target as HTMLTextAreaElement).value.split(/[\n,]/).map(value => value.trim()).filter(Boolean)
}
</script>

<template>
  <div class="space-y-4">
    <div>
      <label :for="`${prefix}-allowlist`" class="block font-medium">License allowlist</label>
      <p :id="`${prefix}-help`" class="text-sm text-muted">{{ override ? 'A nonempty override replaces the inherited allowlist. Empty overrides are rejected.' : 'One SPDX license per line. An empty global allowlist disables license enforcement.' }}</p>
      <textarea :id="`${prefix}-allowlist`" :value="(policy.allowlist || []).join('\n')" :aria-describedby="`${prefix}-help`" class="mt-2 w-full rounded border border-default bg-default p-3" rows="4" @input="setLicenses" />
    </div>
    <div>
      <label :for="`${prefix}-undeclared`" class="block font-medium">Undeclared license behavior</label>
      <select :id="`${prefix}-undeclared`" v-model="policy.undeclared" class="mt-2 rounded border border-default bg-default p-2">
        <option v-if="override" value="">Inherit</option>
        <option value="reject">Reject</option><option value="warn">Warn</option><option value="allow">Allow</option>
      </select>
    </div>
  </div>
</template>
