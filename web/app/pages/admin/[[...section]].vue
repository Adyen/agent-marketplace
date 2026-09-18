<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { AdminError, cloneAdmin, createAdminApi, lifecycleActions, newAdminRegistry, restoreRegistrySettings } from '~/lib/admin'
import type { AdminChange, AdminConfiguration, AdminInstance, AdminProfiles, AdminProposal, AdminRegistry, AdminSession } from '~/lib/admin'

const route = useRoute()
const api = createAdminApi()
const session = ref<AdminSession>()
const configuration = ref<AdminConfiguration>()
const profiles = ref<AdminProfiles>({ profiles: [], source_harnesses: [] })
const instance = ref<AdminInstance>()
const changes = ref<AdminChange[]>([])
const hasOlder = ref(false)
const pending = ref(true)
const error = ref('')
const draft = ref<AdminProposal>()
const editorKey = ref(0)
const search = ref('')
const lifecycleFilter = ref('current')
const parts = computed(() => Array.isArray(route.params.section) ? route.params.section : route.params.section ? [route.params.section] : [])
const section = computed(() => parts.value[0] || 'registries')
const registryID = computed(() => parts.value[1])
const tab = computed(() => parts.value[2] || 'configuration')
const registry = computed(() => configuration.value?.snapshot.registries.find(value => value.id === registryID.value))
const registries = computed(() => (configuration.value?.snapshot.registries || []).filter(value => `${value.slug} ${value.id} ${value.origin}`.toLowerCase().includes(search.value.toLowerCase()) && (!lifecycleFilter.value || (lifecycleFilter.value === 'current' ? !value.retired : value.lifecycle === lifecycleFilter.value))))
const registryChanges = computed(() => changes.value.filter(value => value.snapshot.registries.some(item => item.id === registryID.value)))
const sectionTitles: Record<string, string> = { policies: 'Policies', activity: 'Activity', instance: 'Instance', registries: 'Registries' }
const title = computed(() => registry.value?.slug || sectionTitles[section.value] || 'Administration')
useHead(() => ({ title: `${title.value} · Administration · Agent Marketplace`, meta: [{ name: 'robots', content: 'noindex, nofollow, noarchive' }] }))

function describeError(value: unknown) {
  if (value instanceof AdminError) {
    if (value.code === 'unauthenticated') return 'Your SSO session is missing or expired. Reauthenticate through your deployment and reload. No requests are automatically replayed.'
    if (value.code === 'forbidden') return 'Your authenticated identity is not authorized to administer this Marketplace.'
    return `${value.code}: ${value.message}`
  }
  return 'Administration could not be loaded.'
}
async function load() {
  pending.value = true
  error.value = ''
  try {
    session.value = await api.session()
    configuration.value = await api.configuration()
    profiles.value = await api.profiles()
    if (section.value === 'instance') instance.value = await api.instance()
    if (section.value === 'activity' || registryID.value) await loadActivity()
  }
  catch (value) { error.value = describeError(value) }
  finally { pending.value = false }
}
async function loadActivity(older = false) {
  try {
    const before = older ? changes.value.at(-1)?.revision : undefined
    const result = await api.activity(before)
    changes.value = older ? [...changes.value, ...result.changes] : result.changes
    hasOlder.value = result.changes.length === 25
  }
  catch (value) { error.value = describeError(value) }
}
function edit(proposal: AdminProposal) {
  draft.value = cloneAdmin(proposal)
  editorKey.value++
}
function editRegistry(value: AdminRegistry) { edit({ action: 'upsert_registry', registry: value, reason: '' }) }
function restore(change: AdminChange, id?: string) {
  if (!configuration.value) return
  if (!id) { edit({ action: 'set_policy', license_policy: change.snapshot.license_policy, reason: `Restore policy settings from revision ${change.revision}` }); return }
  const current = configuration.value.snapshot.registries.find(value => value.id === id)
  const previous = change.snapshot.registries.find(value => value.id === id)
  if (current && previous) edit({ action: 'upsert_registry', registry: restoreRegistrySettings(current, previous), reason: `Restore registry settings from revision ${change.revision}` })
}
function accepted(change: AdminChange) {
  configuration.value = { revision: change.revision, snapshot: change.snapshot }
}
function registryPath(id: string, selectedTab = 'configuration') { return `/admin/registries/${encodeURIComponent(id)}/${selectedTab}` }
onMounted(load)
watch(() => route.fullPath, () => { draft.value = undefined; changes.value = []; void load() })
</script>

<template>
  <main id="main-content" class="mx-auto min-h-screen max-w-7xl space-y-6 px-4 py-8 text-default sm:px-6" tabindex="-1">
    <header class="space-y-4 border-b border-default pb-5">
      <div class="flex flex-wrap items-baseline justify-between gap-2"><div><p class="text-sm font-medium text-muted">Marketplace administration</p><h1 class="mt-1 text-3xl font-semibold text-highlighted">{{ title }}</h1></div><p v-if="session" class="text-sm text-muted">{{ session.actor.subject }} · {{ session.actor.issuer }}</p></div>
      <nav aria-label="Administration" class="flex flex-wrap gap-2"><UButton v-for="item in ['registries', 'policies', 'activity', 'instance']" :key="item" :to="`/admin/${item}`" :variant="section === item ? 'solid' : 'outline'" color="neutral" :aria-current="section === item ? 'page' : undefined">{{ item[0]?.toUpperCase() }}{{ item.slice(1) }}</UButton></nav>
      <p class="text-sm text-muted">Private · {{ configuration ? `Desired configuration r${configuration.revision}` : 'Authenticating' }} · Drafts stay in this page only</p>
    </header>
    <div v-if="pending" role="status" aria-busy="true">Loading authenticated administration…</div>
    <div v-else-if="error" role="alert" class="space-y-3 rounded border border-error p-5"><p>{{ error }}</p><UButton color="neutral" variant="outline" @click="load">Reload administration</UButton></div>
    <template v-else-if="session && configuration">
      <AdminChangeEditor v-if="draft" :key="editorKey" :initial="draft" :configuration="configuration" :profiles="profiles" @accepted="accepted" @cancel="draft = undefined" />
      <template v-else-if="section === 'registries' && !registryID">
        <div class="flex flex-wrap items-end justify-between gap-4"><div><label for="registry-search" class="block text-sm font-medium">Search registries</label><input id="registry-search" v-model="search" type="search" class="mt-1 rounded border border-default bg-default p-2"></div><div><label for="lifecycle-filter" class="block text-sm font-medium">Lifecycle</label><select id="lifecycle-filter" v-model="lifecycleFilter" class="mt-1 rounded border border-default bg-default p-2"><option value="current">Current (exclude retired)</option><option value="">All, including retired</option><option v-for="state in [...new Set(configuration.snapshot.registries.map(value => value.lifecycle))]" :key="state">{{ state }}</option></select></div><UButton @click="editRegistry(newAdminRegistry())">Add registry</UButton></div>
        <div class="overflow-x-auto rounded-xl border border-default"><table class="w-full text-left"><caption class="sr-only">Managed registries, lifecycle and observed publication status</caption><thead class="bg-muted"><tr><th class="p-3">Registry</th><th class="p-3">Configured ref</th><th class="p-3">Lifecycle</th><th class="p-3">Generation</th><th class="p-3">Publication health</th></tr></thead><tbody><tr v-for="item in registries" :key="item.id" class="border-t border-default"><th class="p-3"><NuxtLink :to="registryPath(item.id)" class="text-primary underline">{{ item.slug }}</NuxtLink><p class="text-xs font-normal text-muted">{{ item.id }}</p></th><td class="p-3">{{ item.ref || 'Default branch' }}</td><td class="p-3">{{ item.lifecycle }}</td><td class="p-3">{{ item.generation }}</td><td class="p-3 text-sm text-muted">Not reported by admin API</td></tr></tbody></table></div>
        <p v-if="!registries.length">No registries match. Add a registry or change the filter.</p>
        <p class="text-sm text-muted">Lifecycle is separate from indexing health. Indexed commit, last-success time, and published configuration revision are not currently reported here.</p>
      </template>
      <template v-else-if="section === 'registries' && registry">
        <div class="rounded-xl border border-default p-5"><dl class="grid gap-3 text-sm sm:grid-cols-3"><div><dt class="font-medium">Immutable ID</dt><dd class="break-all">{{ registry.id }}</dd></div><div><dt class="font-medium">Lifecycle</dt><dd>{{ registry.lifecycle }}</dd></div><div><dt class="font-medium">Configured ref</dt><dd>{{ registry.ref || 'Default branch' }}</dd></div><div><dt class="font-medium">Desired revision / generation</dt><dd>r{{ configuration.revision }} / {{ registry.generation }}</dd></div><div><dt class="font-medium">Indexed commit</dt><dd>Not reported</dd></div><div><dt class="font-medium">Published configuration revision</dt><dd>Not reported</dd></div></dl></div>
        <nav aria-label="Registry sections" class="flex flex-wrap gap-2"><UButton v-for="item in [{ id: 'resources', label: 'Resources' }, { id: 'configuration', label: 'Configuration' }, { id: 'indexing', label: 'Indexing history' }, { id: 'findings', label: 'Findings' }]" :key="item.id" :to="registryPath(registry.id, item.id)" :variant="tab === item.id ? 'solid' : 'outline'" color="neutral" :aria-current="tab === item.id ? 'page' : undefined">{{ item.label }}</UButton></nav>
        <section v-if="tab === 'configuration'" class="space-y-5">
          <dl class="grid gap-4 rounded-xl border border-default p-5 sm:grid-cols-2"><div><dt class="font-medium">Origin</dt><dd class="break-all">{{ registry.origin }}</dd></div><div><dt class="font-medium">Credential / trust reference</dt><dd>{{ registry.profile_id || 'None' }}</dd></div><div><dt class="font-medium">Ordered roots</dt><dd><ol class="list-inside list-decimal"><li v-for="(root, index) in registry.roots" :key="index">{{ root.path }} · {{ root.source_harness || 'Autodetect' }}</li></ol></dd></div><div><dt class="font-medium">Acquisition</dt><dd>{{ registry.acquisition }}</dd></div><div><dt class="font-medium">Publisher</dt><dd>{{ registry.publisher.name || 'Not set' }}</dd></div><div><dt class="font-medium">Default license</dt><dd>{{ registry.license || 'Not set' }}</dd></div></dl>
          <UButton @click="editRegistry(registry)">Edit configuration</UButton>
          <details class="rounded border border-default p-4"><summary class="cursor-pointer font-medium">Lifecycle and indexing actions</summary><div class="mt-4 grid gap-4 sm:grid-cols-2"><div v-for="action in lifecycleActions" :key="action.action" class="space-y-2"><UButton color="neutral" variant="outline" @click="edit({ action: action.action, registry_id: registry.id, reason: '' })">{{ action.label }}</UButton><p class="text-sm text-muted">{{ action.description }}</p></div></div></details>
          <details><summary class="cursor-pointer">Read-only managed representation (JSON, valid YAML 1.2)</summary><pre class="mt-3 overflow-auto rounded bg-muted p-4 text-sm">{{ JSON.stringify(registry, null, 2) }}</pre></details>
        </section>
        <section v-else-if="tab === 'resources'" class="space-y-4 rounded-xl border border-default p-5"><h2 class="text-xl font-semibold">Published resources</h2><p>The public catalog shows currently discoverable resources, not all acquired or blocked content.</p><UButton :to="{ path: '/', query: { registry: registry.slug } }" color="neutral" variant="outline">Browse published resources</UButton><p class="text-sm text-muted">Admin resource discovery and unavailable-resource diagnostics are not exposed by this API yet.</p></section>
        <section v-else-if="tab === 'findings'" class="space-y-3 rounded-xl border border-default p-5"><h2 class="text-xl font-semibold">Findings</h2><p>Structured discovery, assessment, license, withdrawal, and artifact findings are not available from the admin API yet. This is not evidence that the registry has no findings.</p><p>License settings: {{ registry.license_override ? 'Registry override' : 'Inherited global policy' }}. Lifecycle: {{ registry.lifecycle }}. Resource availability must be evaluated by the backend.</p><UButton to="/admin/policies" color="neutral" variant="outline">Inspect policy configuration</UButton></section>
        <section v-else-if="tab === 'indexing'" class="space-y-4"><h2 class="text-xl font-semibold">Indexing history</h2><p class="text-sm text-muted">Jobs linked from the loaded configuration history are shown below. This is not an exhaustive attempt or publication history. These are instance-wide changes while this registry existed; linked jobs may concern other registries.</p><article v-for="change in registryChanges" :key="`${change.revision}-${change.request_id}`" class="space-y-3 rounded border border-default p-4"><h3 class="font-medium">r{{ change.revision }} · {{ change.operation }}</h3><p>{{ change.created_at }} · {{ change.reason }}</p><AdminJobs :ids="change.job_ids || []" /><UButton color="neutral" variant="outline" @click="restore(change, registry.id)">Prepare registry settings from r{{ change.revision }}</UButton></article><UButton v-if="hasOlder" color="neutral" variant="outline" @click="loadActivity(true)">Load older changes</UButton><p v-if="!registryChanges.length">No linked changes in this page of history.</p></section>
        <p v-else>Unknown registry section.</p>
      </template>
      <section v-else-if="section === 'policies'" class="space-y-5">
        <div class="space-y-3 rounded-xl border border-default p-5"><h2 class="text-xl font-semibold">Global license policy</h2><p><strong>Allowlist:</strong> {{ configuration.snapshot.license_policy.allowlist?.join(', ') || 'Empty — enforcement disabled' }}</p><p><strong>Undeclared licenses:</strong> {{ configuration.snapshot.license_policy.undeclared }}</p><UButton @click="edit({ action: 'set_policy', license_policy: configuration.snapshot.license_policy, reason: '' })">Edit license policy</UButton></div>
        <h2 class="text-xl font-semibold">Registry inheritance and overrides</h2><ul class="space-y-3"><li v-for="item in configuration.snapshot.registries" :key="item.id" class="rounded border border-default p-4"><NuxtLink :to="registryPath(item.id)" class="font-medium text-primary underline">{{ item.slug }}</NuxtLink><p>{{ item.license_override ? `Override allowlist: ${item.license_override.allowlist?.join(', ') || 'Inherited'}; undeclared: ${item.license_override.undeclared || 'Inherited'}` : 'Inherits global license policy' }}</p></li></ul>
        <p class="text-sm text-muted">License impact analysis is unavailable; no exhaustive affected-resource or retained-revision counts are claimed. Hidden-content analysis and resource/time limits remain deployment-managed.</p>
      </section>
      <section v-else-if="section === 'activity'" class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-xl font-semibold">Configuration and operational requests</h2><UButton color="neutral" variant="outline" @click="loadActivity()">Refresh activity</UButton></div>
        <article v-for="change in changes" :key="`${change.revision}-${change.request_id}`" class="space-y-4 rounded-xl border border-default p-5"><h3 class="font-semibold">r{{ change.parent_revision }} → r{{ change.revision }} · {{ change.operation }}</h3><p>{{ change.actor.subject }} · {{ change.actor.issuer }} · {{ change.created_at }}</p><p>{{ change.reason }}</p><p class="break-all text-sm text-muted">Request {{ change.request_id }}</p><AdminJobs :ids="change.job_ids || []" /><details><summary class="cursor-pointer">Historical managed settings</summary><pre class="mt-2 overflow-auto rounded bg-muted p-3 text-sm">{{ JSON.stringify(change.snapshot, null, 2) }}</pre></details><div class="flex flex-wrap gap-2"><UButton color="neutral" variant="outline" @click="restore(change)">Prepare policy settings from r{{ change.revision }}</UButton><UButton v-for="item in change.snapshot.registries.filter(old => configuration?.snapshot.registries.some(current => current.id === old.id))" :key="item.id" color="neutral" variant="outline" @click="restore(change, item.id)">Prepare {{ item.slug }} settings</UButton></div><p class="text-sm text-muted">Restoration creates a new draft against current settings. Lifecycle, aliases, historical origins, and non-license overrides are preserved.</p></article>
        <p v-if="!changes.length">No changes returned.</p><UButton v-if="hasOlder" color="neutral" variant="outline" @click="loadActivity(true)">Load older changes</UButton>
      </section>
      <section v-else-if="section === 'instance' && instance" class="space-y-4 rounded-xl border border-default p-5"><h2 class="text-xl font-semibold">Deployment-owned instance · read only</h2><dl class="grid gap-4 sm:grid-cols-2"><div><dt class="font-medium">Name</dt><dd>{{ instance.name }}</dd></div><div><dt class="font-medium">Version</dt><dd>{{ instance.version }}</dd></div><div><dt class="font-medium">Management mode</dt><dd>{{ instance.management_mode }}</dd></div><div><dt class="font-medium">Configuration revision</dt><dd>{{ instance.revision }}</dd></div><div><dt class="font-medium">Worker observation</dt><dd>{{ instance.worker_status || 'No recent worker activity reported' }}</dd></div></dl><p class="text-sm text-muted">No heartbeat is inferred from queue activity. Storage, identity trust, networking, profiles, and secret provisioning are deployment-managed and cannot be edited here.</p></section>
      <p v-else role="alert">This administration page or registry does not exist.</p>
    </template>
  </main>
</template>
