<script setup lang="ts">
import { computed } from 'vue'
import type { AdminRegistry, AdminProfiles } from '~/lib/admin'
const registry = defineModel<AdminRegistry>({ required: true })
// `errors` is keyed by the draft paths the API reports in `fields`
// (id, slug, origin, ref, acquisition, profile_id, license_override,
// roots[N].path, roots[N].source_harness, publisher.url, license).
const props = defineProps<{ profiles: AdminProfiles, existing: boolean, errors?: Record<string, string> }>()
const acquisitionItems = [{ label: 'Index only', value: 'index-only' }, { label: 'Mirror', value: 'mirror' }]
const harnessItems = computed(() => [{ label: 'Autodetect', value: '' }, ...props.profiles.source_harnesses.map(value => ({ label: value, value }))])
const profileItems = computed(() => [{ label: 'No profile', value: '' }, ...props.profiles.profiles.map(profile => ({ label: `${profile.id} — ${profile.allowed_origins.join(', ')}`, value: profile.id }))])
const control = 'w-full rounded-md border border-default bg-default px-2.5 py-1.5 text-sm text-highlighted focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-primary disabled:opacity-60'
function problem(path: string) { return props.errors?.[path] }
function invalid(path: string) { return problem(path) ? 'true' as const : undefined }
function describedBy(id: string, path: string) { return problem(path) ? `${id}-help` : undefined }
function moveRoot(index: number, direction: number) {
  const roots = registry.value.roots || []
  const target = index + direction
  if (target < 0 || target >= roots.length) return
  const root = roots.splice(index, 1)[0]
  if (root) roots.splice(target, 0, root)
}
// Optional string fields may be absent from the API payload; a native select
// needs an explicit '' to show its "none" option.
function selected(event: Event) { return (event.target as HTMLSelectElement).value }
function setOverride(event: Event) {
  if ((event.target as HTMLInputElement).checked) registry.value.license_override = { allowlist: [], undeclared: '' }
  else delete registry.value.license_override
}
</script>

<template>
  <div class="space-y-6">
    <section class="space-y-4" aria-labelledby="identity-heading">
      <h3 id="identity-heading" class="text-sm font-semibold uppercase tracking-wide text-muted">Identity and source</h3>
      <div class="grid gap-4 sm:grid-cols-2">
        <AdminField id="registry-id" label="Immutable registry ID" required :description="existing ? 'Identity cannot be changed once created.' : 'Stable machine identity; the slug can change later, this cannot.'" :error="problem('id')"><UInput id="registry-id" v-model="registry.id" :readonly="existing" required placeholder="team-skills" class="w-full font-mono" :ui="{ base: existing ? 'bg-[var(--am-color-bg-subtle)] text-muted' : '' }" :aria-invalid="invalid('id')" :aria-describedby="describedBy('registry-id', 'id')" /></AdminField>
        <AdminField id="registry-slug" label="Slug" required description="Shown to users in the catalog and install commands. Lowercase letters, digits and hyphens only." :error="problem('slug')"><UInput id="registry-slug" v-model="registry.slug" required placeholder="team" class="w-full" :aria-invalid="invalid('slug')" :aria-describedby="describedBy('registry-slug', 'slug')" /></AdminField>
        <AdminField id="registry-origin" label="Git origin" required description="Approved HTTPS or SSH origin only. Never include credentials or server paths." class="sm:col-span-2" :error="problem('origin')"><UInput id="registry-origin" v-model="registry.origin" required placeholder="https://git.example/org/repo" class="w-full font-mono" :aria-invalid="invalid('origin')" :aria-describedby="describedBy('registry-origin', 'origin')" /></AdminField>
        <AdminField id="registry-ref" label="Ref or exact commit" required description="Branch, tag or commit SHA that indexing checks out." :error="problem('ref')"><UInput id="registry-ref" v-model="registry.ref" required placeholder="main" class="w-full font-mono" :aria-invalid="invalid('ref')" :aria-describedby="describedBy('registry-ref', 'ref')" /></AdminField>
        <AdminField id="registry-acquisition" label="Acquisition" description="Mirror keeps a server-side copy; index only reads metadata." :error="problem('acquisition')"><USelect id="registry-acquisition" v-model="registry.acquisition" :items="acquisitionItems" class="w-full" :aria-invalid="invalid('acquisition')" /></AdminField>
      </div>
    </section>

    <fieldset class="space-y-3 rounded-lg border p-4" :class="problem('roots') ? 'border-error/60' : 'border-default'">
      <legend class="px-1 text-sm font-semibold text-highlighted">Source roots</legend>
      <p v-if="problem('roots')" role="alert" class="text-sm text-error">{{ problem('roots') }}</p>
      <p class="text-xs text-muted">Directories inside the repository to scan, in priority order. Reorder with the arrow buttons.</p>
      <ol class="space-y-2">
        <li v-for="(root, index) in registry.roots" :key="index" class="flex flex-wrap items-end gap-2 rounded-md bg-[var(--am-color-bg-subtle)] p-2 sm:flex-nowrap">
          <span class="pb-2 text-xs text-muted" aria-hidden="true">{{ index + 1 }}.</span>
          <AdminField :id="`root-${index}`" :label="`Root ${index + 1} path`" class="min-w-40 grow" :error="problem(`roots[${index}].path`)"><UInput :id="`root-${index}`" v-model="root.path" required placeholder="." class="w-full font-mono" :aria-invalid="invalid(`roots[${index}].path`)" :aria-describedby="describedBy(`root-${index}`, `roots[${index}].path`)" /></AdminField>
          <AdminField :id="`harness-${index}`" label="Source harness" class="min-w-40" :error="problem(`roots[${index}].source_harness`)"><select :id="`harness-${index}`" :value="root.source_harness || ''" :class="control" :aria-invalid="invalid(`roots[${index}].source_harness`)" @change="root.source_harness = selected($event)"><option v-for="item in harnessItems" :key="item.value" :value="item.value">{{ item.label }}</option></select></AdminField>
          <div class="flex gap-1">
            <UButton type="button" color="neutral" variant="ghost" size="sm" icon="i-lucide-arrow-up" :disabled="index === 0" :aria-label="`Move root ${index + 1} up`" @click="moveRoot(index, -1)" />
            <UButton type="button" color="neutral" variant="ghost" size="sm" icon="i-lucide-arrow-down" :disabled="index === (registry.roots?.length || 0) - 1" :aria-label="`Move root ${index + 1} down`" @click="moveRoot(index, 1)" />
            <UButton type="button" color="error" variant="ghost" size="sm" icon="i-lucide-trash-2" :aria-label="`Remove root ${index + 1}`" @click="registry.roots?.splice(index, 1)" />
          </div>
        </li>
      </ol>
      <UButton type="button" color="neutral" variant="outline" size="sm" icon="i-lucide-plus" @click="(registry.roots ||= []).push({ path: '', source_harness: '' })">Add source root</UButton>
    </fieldset>

    <section class="space-y-4" aria-labelledby="publisher-heading">
      <h3 id="publisher-heading" class="text-sm font-semibold uppercase tracking-wide text-muted">Trust and publisher</h3>
      <div class="grid gap-4 sm:grid-cols-2">
        <AdminField id="registry-profile" label="Credential / trust profile" description="Deployment-approved reference only. Secrets cannot be viewed or edited here." class="sm:col-span-2" :error="problem('profile_id')"><select id="registry-profile" :value="registry.profile_id || ''" :class="control" :aria-invalid="invalid('profile_id')" :aria-describedby="describedBy('registry-profile', 'profile_id')" @change="registry.profile_id = selected($event)"><option v-for="item in profileItems" :key="item.value" :value="item.value">{{ item.label }}</option></select></AdminField>
        <AdminField id="publisher-name" label="Publisher name" :error="problem('publisher.name')"><UInput id="publisher-name" v-model="registry.publisher.name" placeholder="Platform team" class="w-full" :aria-invalid="invalid('publisher.name')" :aria-describedby="describedBy('publisher-name', 'publisher.name')" /></AdminField>
        <AdminField id="publisher-url" label="Publisher URL" :error="problem('publisher.url')"><UInput id="publisher-url" v-model="registry.publisher.url" type="url" placeholder="https://example.com/team" class="w-full font-mono" :aria-invalid="invalid('publisher.url')" :aria-describedby="describedBy('publisher-url', 'publisher.url')" /></AdminField>
        <AdminField id="registry-license" label="Default license" description="SPDX identifier used when a resource declares none." :error="problem('license')"><UInput id="registry-license" v-model="registry.license" placeholder="MIT" class="w-full font-mono" :aria-invalid="invalid('license')" :aria-describedby="describedBy('registry-license', 'license')" /></AdminField>
      </div>
    </section>

    <AdminField id="rejected-resources" label="Rejected resource availability" description="Override whether rejected resources from this registry are downloadable and installable." :error="problem('rejected_resources')">
      <select id="rejected-resources" v-model="registry.rejected_resources" :class="control"><option value="">Inherit global policy</option><option value="block">Block</option><option value="expose">Expose as operator-approved</option></select>
    </AdminField>

    <section class="space-y-3 rounded-lg border p-4" :class="problem('license_override') ? 'border-error/60' : 'border-default'" aria-labelledby="override-heading">
      <label id="override-heading" class="flex items-center gap-2 text-sm font-semibold text-highlighted"><input type="checkbox" :checked="!!registry.license_override" class="size-4 accent-[var(--ui-primary)]" @change="setOverride">Override the inherited license policy for this registry</label>
      <AdminPolicyFields v-if="registry.license_override" v-model="registry.license_override" prefix="override" override :error="problem('license_override')" />
    </section>

    <AdminHint summary="Preserved read-only settings"><p>Aliases, historical origins, lifecycle and non-license overrides are kept as they are and cannot be edited in this form.</p><pre class="overflow-auto rounded bg-[var(--am-color-bg-subtle)] p-3 font-mono text-xs">{{ JSON.stringify({ aliases: registry.aliases, historical_origins: registry.historical_origins, non_license_overrides: registry.non_license_overrides }, null, 2) }}</pre></AdminHint>
  </div>
</template>
