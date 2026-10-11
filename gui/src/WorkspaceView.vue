<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { createClient } from './api/client'
import { launchRequestEvent, resolveBootstrap } from './bootstrap'
import { eligible, GROUP_ORDER, WorkspaceController, groupKey, groupLabel, type Row } from './controller'
import FirstRun from './FirstRun.vue'
import HarnessSettings from './HarnessSettings.vue'
import SourcesSettings from './SourcesSettings.vue'

const props = defineProps<{ controller?: WorkspaceController }>()

const state = shallowRef<WorkspaceController | null>(props.controller ?? null)
const selectedId = ref<string | null>(null)
const error = ref<string | null>(null)
const settingsOpen = ref(false)
const settingsButton = ref<HTMLButtonElement | null>(null)
const sourcesOpen = ref(false)
const sourcesButton = ref<HTMLButtonElement | null>(null)
const setupOpen = ref(false)
const setupDismissed = ref(false)
const setupOpened = ref(false)
const setupButton = ref<HTMLButtonElement | null>(null)

const configuration = computed(() => state.value?.state.configuration ?? null)

watch(settingsOpen, (isOpen, wasOpen) => {
  if (!isOpen && wasOpen) {
    void nextTick(() => settingsButton.value?.focus())
  }
})

watch(sourcesOpen, (isOpen, wasOpen) => {
  if (!isOpen && wasOpen) {
    void nextTick(() => sourcesButton.value?.focus())
  }
})

watch(setupOpen, (isOpen, wasOpen) => {
  if (isOpen) {
    setupOpened.value = true
    return
  }
  if (wasOpen) {
    void nextTick(() => (setupButton.value ?? settingsButton.value)?.focus())
  }
})

watch(
  () => configuration.value?.firstRun,
  (firstRun) => {
    if (firstRun === true) {
      if (!setupDismissed.value && !setupOpen.value) {
        setupOpen.value = true
      }
    } else if (firstRun === false) {
      setupDismissed.value = false
      if (setupOpened.value) {
        setupOpened.value = false
        void nextTick(() => settingsButton.value?.focus())
      }
    }
  },
)

function onFocus() {
  void state.value?.refresh()
}

function onSetupCancel() {
  setupDismissed.value = true
}

let detachLaunchRequest: (() => void) | void

onMounted(async () => {
  window.addEventListener('focus', onFocus)
  detachLaunchRequest = window.runtime?.EventsOn?.(launchRequestEvent, onFocus)
  if (state.value) {
    void state.value.loadConfiguration()
    return
  }
  try {
    const bootstrap = await resolveBootstrap()
    const c = new WorkspaceController(createClient(bootstrap.url, bootstrap.credential))
    state.value = c
    void c.loadConfiguration()
    c.connect().catch(() => {
      error.value = 'Could not connect to the session'
    })
  } catch {
    error.value = 'Could not start the workspace'
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('focus', onFocus)
  detachLaunchRequest?.()
  detachLaunchRequest = undefined
  state.value?.dispose()
})

const view = computed(() => state.value?.state)

const groups = computed(() => {
  const rows = view.value?.rows ?? []
  const byGroup = new Map<string, Row[]>()
  for (const row of rows) {
    const key = groupKey(row)
    const list = byGroup.get(key) ?? []
    list.push(row)
    byGroup.set(key, list)
  }
  return GROUP_ORDER.filter((key) => byGroup.has(key)).map((key) => ({
    key,
    label: groupLabel(key),
    rows: byGroup.get(key) ?? [],
  }))
})

const orderedRows = computed(() => groups.value.flatMap((group) => group.rows))

const selected = computed(() => (view.value?.rows ?? []).find((row) => row.id === selectedId.value) ?? null)

function select(row: Row) {
  selectedId.value = row.id
}

async function moveSelection(direction: 1 | -1) {
  const rows = orderedRows.value
  if (rows.length === 0) {
    return
  }
  const index = rows.findIndex((row) => row.id === selectedId.value)
  const next = index === -1 ? 0 : Math.min(rows.length - 1, Math.max(0, index + direction))
  selectedId.value = rows[next].id
  await nextTick()
  document.getElementById(`row-${rows[next].id}`)?.focus()
}

const busy = computed(() => view.value == null || view.value.operation != null || view.value.requesting)
const ready = computed(() => view.value != null && eligible(view.value))

async function refresh() {
  await state.value?.refresh()
}

async function run(action: 'sync' | 'update') {
  await state.value?.runAction(action)
}
</script>

<template>
  <div class="workspace">
    <header class="workspace__header">
      <h1>Agent Marketplace</h1>
      <div class="workspace__actions" role="toolbar" aria-label="Session actions">
        <button type="button" aria-label="Refresh" :disabled="busy" @click="refresh">Refresh</button>
        <button type="button" aria-label="Sync" :disabled="!ready" @click="run('sync')">Sync</button>
        <button type="button" aria-label="Update" :disabled="!ready" @click="run('update')">Update</button>
        <button
          v-if="configuration?.firstRun === true || setupOpen"
          ref="setupButton"
          type="button"
          aria-label="Set up"
          @click="setupOpen = true"
        >Set up</button>
        <button
          ref="sourcesButton"
          type="button"
          aria-label="Sources"
          @click="sourcesOpen = true"
        >Sources</button>
        <button
          ref="settingsButton"
          type="button"
          aria-label="Settings"
          @click="settingsOpen = true"
        >Settings</button>
      </div>
    </header>

    <p v-if="error || view?.error" class="workspace__error" role="alert">{{ error ?? view?.error }}</p>
    <p v-else-if="view && view.stage !== '' && view.harnesses.length === 0" class="workspace__notice" role="status">
      No supported target harnesses are configured.
    </p>
    <p v-else-if="view?.operation" class="workspace__notice" role="status">
      {{ view.operation.kind }} {{ view.phase ?? 'running' }}
    </p>

    <div class="workspace__panes">
      <section aria-label="Resources" class="workspace__list">
        <div
          v-for="group in groups"
          :key="group.key"
          class="workspace__group"
        >
          <h2>{{ group.label }}</h2>
          <ul role="listbox" aria-label="Resources">
            <li
              v-for="row in group.rows"
              :id="`row-${row.id}`"
              :key="row.id"
              role="option"
              tabindex="0"
              :aria-selected="selectedId === row.id"
              :class="{ 'workspace__row--selected': selectedId === row.id }"
              class="workspace__row"
              @click="select(row)"
              @keydown.enter="select(row)"
              @keydown.space.prevent="select(row)"
              @keydown.down.prevent="moveSelection(1)"
              @keydown.up.prevent="moveSelection(-1)"
            >
              <span class="workspace__check" aria-hidden="true">{{ row.checked ? '[x]' : '[ ]' }}</span>
              <span class="workspace__name">{{ row.registry }}/{{ row.kind }}/{{ row.name }}</span>
              <span class="workspace__scope">{{ row.scope }}</span>
              <span v-if="row.harnessOverride" class="workspace__badge" title="Registry-level harness override">harnesses: {{ row.harnesses.join(', ') }}</span>
            </li>
          </ul>
        </div>
      </section>

      <aside aria-label="Detail" class="workspace__detail">
        <template v-if="selected">
          <h2>{{ selected.registry }}/{{ selected.kind }}/{{ selected.name }}</h2>
          <p>{{ selected.description }}</p>
          <dl>
            <dt>Registry</dt>
            <dd class="mono">{{ selected.registry }}</dd>
            <dt>Scope</dt>
            <dd>{{ selected.scope }}</dd>
            <dt>Status</dt>
            <dd>{{ selected.status }}</dd>
            <dt>Harnesses</dt>
            <dd class="mono">{{ selected.harnesses.join(', ') || 'none' }}</dd>
          </dl>
        </template>
        <p v-else>Select a resource to inspect it.</p>

        <h2>Session</h2>
        <p>Drift: {{ view?.drift }}</p>
        <p>Harnesses: <span class="mono">{{ (view?.harnesses ?? []).join(', ') || 'none' }}</span></p>

        <h2>Diagnostics</h2>
        <ul v-if="(view?.diagnostics?.length ?? 0) > 0" aria-label="Diagnostics">
          <li v-for="d in view?.diagnostics" :key="d.code" :class="`diag--${d.severity}`">
            {{ d.severity }}: {{ d.message }}
          </li>
        </ul>
        <p v-else>None</p>
      </aside>
    </div>

    <HarnessSettings v-if="state" v-model:open="settingsOpen" :controller="state" />
    <SourcesSettings v-if="state" v-model:open="sourcesOpen" :controller="state" />
    <FirstRun v-if="state" v-model:open="setupOpen" :controller="state" @cancel="onSetupCancel" />

    <ul class="workspace__notifications" aria-live="polite" aria-label="Notifications">
      <li v-for="n in view?.notifications" :key="n.id" :class="`notification--${n.severity}`">
        {{ n.message }}
      </li>
    </ul>
  </div>
</template>
