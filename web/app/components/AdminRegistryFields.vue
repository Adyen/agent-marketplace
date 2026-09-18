<script setup lang="ts">
import type { AdminRegistry, AdminProfiles } from '~/lib/admin'
const registry = defineModel<AdminRegistry>({ required: true })
defineProps<{ profiles: AdminProfiles, existing: boolean }>()
function moveRoot(index: number, direction: number) {
  const roots = registry.value.roots || []
  const target = index + direction
  if (target < 0 || target >= roots.length) return
  const root = roots.splice(index, 1)[0]
  if (root) roots.splice(target, 0, root)
}
function setOverride(event: Event) {
  if ((event.target as HTMLInputElement).checked) registry.value.license_override = { allowlist: [], undeclared: '' }
  else delete registry.value.license_override
}
</script>

<template>
  <div class="space-y-5">
    <div class="grid gap-4 sm:grid-cols-2">
      <div><label for="registry-id" class="block font-medium">Immutable registry ID</label><input id="registry-id" v-model="registry.id" :readonly="existing" required class="mt-1 w-full rounded border border-default bg-default p-2" :aria-describedby="existing ? 'immutable-help' : undefined"><p v-if="existing" id="immutable-help" class="text-sm text-muted">Identity cannot be changed.</p></div>
      <div><label for="registry-slug" class="block font-medium">Slug</label><input id="registry-slug" v-model="registry.slug" required class="mt-1 w-full rounded border border-default bg-default p-2"></div>
      <div class="sm:col-span-2"><label for="registry-origin" class="block font-medium">Git origin</label><input id="registry-origin" v-model="registry.origin" required class="mt-1 w-full rounded border border-default bg-default p-2" aria-describedby="origin-help"><p id="origin-help" class="text-sm text-muted">Approved HTTPS or SSH origin only. Never include credentials or server paths.</p></div>
      <div><label for="registry-ref" class="block font-medium">Ref or exact commit</label><input id="registry-ref" v-model="registry.ref" class="mt-1 w-full rounded border border-default bg-default p-2"></div>
      <div><label for="registry-acquisition" class="block font-medium">Acquisition</label><select id="registry-acquisition" v-model="registry.acquisition" class="mt-1 w-full rounded border border-default bg-default p-2"><option value="index-only">Index only</option><option value="mirror">Mirror</option></select></div>
    </div>
    <fieldset class="space-y-3 rounded border border-default p-4">
      <legend class="px-1 font-medium">Ordered source roots</legend>
      <p class="text-sm text-muted">Order is significant. Use Move up and Move down with the keyboard.</p>
      <div v-for="(root, index) in registry.roots" :key="index" class="flex flex-wrap items-end gap-2">
        <div class="grow"><label :for="`root-${index}`" class="block">Root {{ index + 1 }} path</label><input :id="`root-${index}`" v-model="root.path" required class="w-full rounded border border-default bg-default p-2"></div>
        <div><label :for="`harness-${index}`" class="block">Source harness</label><select :id="`harness-${index}`" v-model="root.source_harness" class="rounded border border-default bg-default p-2"><option value="">Autodetect</option><option v-for="harness in profiles.source_harnesses" :key="harness" :value="harness">{{ harness }}</option></select></div>
        <UButton type="button" color="neutral" variant="outline" :disabled="index === 0" :aria-label="`Move root ${index + 1} up`" @click="moveRoot(index, -1)">Move up</UButton>
        <UButton type="button" color="neutral" variant="outline" :disabled="index === (registry.roots?.length || 0) - 1" :aria-label="`Move root ${index + 1} down`" @click="moveRoot(index, 1)">Move down</UButton>
        <UButton type="button" color="neutral" variant="outline" :aria-label="`Remove root ${index + 1}`" @click="registry.roots?.splice(index, 1)">Remove</UButton>
      </div>
      <UButton type="button" color="neutral" variant="outline" @click="(registry.roots ||= []).push({ path: '', source_harness: '' })">Add source root</UButton>
    </fieldset>
    <div><label for="registry-profile" class="block font-medium">Credential / trust profile reference</label><select id="registry-profile" v-model="registry.profile_id" class="mt-1 w-full rounded border border-default bg-default p-2"><option value="">No profile</option><option v-for="profile in profiles.profiles" :key="profile.id" :value="profile.id">{{ profile.id }} — {{ profile.allowed_origins.join(', ') }}</option></select><p class="text-sm text-muted">Opaque deployment-approved references only. Secrets cannot be viewed or edited.</p></div>
    <div class="grid gap-4 sm:grid-cols-2">
      <div><label for="publisher-name" class="block font-medium">Publisher name</label><input id="publisher-name" v-model="registry.publisher.name" class="mt-1 w-full rounded border border-default bg-default p-2"></div>
      <div><label for="publisher-url" class="block font-medium">Publisher URL</label><input id="publisher-url" v-model="registry.publisher.url" type="url" class="mt-1 w-full rounded border border-default bg-default p-2"></div>
      <div><label for="registry-license" class="block font-medium">Default license</label><input id="registry-license" v-model="registry.license" class="mt-1 w-full rounded border border-default bg-default p-2"></div>
    </div>
    <label class="flex items-center gap-2"><input type="checkbox" :checked="!!registry.license_override" @change="setOverride">Override inherited license policy</label>
    <AdminPolicyFields v-if="registry.license_override" v-model="registry.license_override" prefix="override" override />
    <details><summary class="cursor-pointer font-medium">Preserved read-only settings</summary><p class="my-2 text-sm text-muted">Aliases, historical origins, lifecycle, and non-license overrides are not edited here.</p><pre class="overflow-auto rounded bg-muted p-3 text-sm">{{ JSON.stringify({ aliases: registry.aliases, historical_origins: registry.historical_origins, non_license_overrides: registry.non_license_overrides }, null, 2) }}</pre></details>
  </div>
</template>
