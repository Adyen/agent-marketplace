<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { createClient } from './api/client'
import { closeRequestEvent, launchRequestEvent, nativeQuit, resolveBootstrap } from './bootstrap'
import { eligible, WorkspaceController, type Row, type WorkspaceState } from './controller'
import { compareRows, hasSearchTerms, highlightNameParts, rankRows, type NameSegment, type SearchMatch } from './search'
import Confirmations from './Confirmations.vue'
import FirstRun from './FirstRun.vue'
import HarnessSettings from './HarnessSettings.vue'
import ResourceActions from './ResourceActions.vue'
import ResourceInspection from './ResourceInspection.vue'
import SourcesSettings from './SourcesSettings.vue'

type GroupMetadata = WorkspaceState['groups'][number]
type GroupView = GroupMetadata & { rows: Row[]; collapsed: boolean }
type VisibleEntry =
  | { key: string; kind: 'section'; elementId: string }
  | { key: string; kind: 'row'; elementId: string; row: Row }
type ViewportAnchor = { key: string; top: number }
type SearchFoldState = { query: string; groups: Record<string, boolean> }
type PendingResourceSelection = { rowId: string; selectedKey: string; operationId: number | null; nextKey: string | null; previous: VisibleEntry[] }

const props = defineProps<{ controller?: WorkspaceController }>()

const state = shallowRef<WorkspaceController | null>(props.controller ?? null)
const selectedKey = ref<string | null>(null)
const selectionTouched = ref(false)
const foldedGroups = ref<Record<string, boolean>>({})
const temporarySearchFolds = ref<SearchFoldState>({ query: '', groups: {} })
const activeQuery = ref('')
const draftQuery = ref('')
const appliedQuery = ref('')
const searchEditing = ref(false)
const searchButton = ref<HTMLButtonElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const listRoot = ref<HTMLElement | null>(null)
const error = ref<string | null>(null)
const settingsOpen = ref(false)
const settingsButton = ref<HTMLButtonElement | null>(null)
const sourcesOpen = ref(false)
const sourcesButton = ref<HTMLButtonElement | null>(null)
const setupOpen = ref(false)
const setupDismissed = ref(false)
const setupOpened = ref(false)
const setupButton = ref<HTMLButtonElement | null>(null)
const openResourceButton = ref<HTMLButtonElement | null>(null)
const resourceActionButton = ref<HTMLButtonElement | null>(null)
const inspectionOpen = ref(false)
const quitOpen = ref(false)

const configuration = computed(() => state.value?.state.configuration ?? null)

watch(searchEditing, (isEditing, wasEditing) => {
  if (isEditing) {
    void nextTick(() => searchInput.value?.focus())
  } else if (wasEditing) {
    void nextTick(() => searchButton.value?.focus())
  }
})

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

watch(inspectionOpen, (isOpen, wasOpen) => {
  if (!isOpen && wasOpen) {
    void nextTick(() => openResourceButton.value?.focus())
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

function openSearch() {
  if (searchEditing.value) {
    return
  }
  draftQuery.value = appliedQuery.value
  activeQuery.value = appliedQuery.value
  searchEditing.value = true
}

function updateSearch(event: Event) {
  selectionTouched.value = true
  pendingResourceSelection = null
  const query = (event.currentTarget as HTMLInputElement).value
  draftQuery.value = query
  activeQuery.value = query
}

function confirmSearch() {
  selectionTouched.value = true
  pendingResourceSelection = null
  appliedQuery.value = draftQuery.value
  activeQuery.value = appliedQuery.value
  searchEditing.value = false
}

function cancelSearch() {
  selectionTouched.value = true
  pendingResourceSelection = null
  draftQuery.value = appliedQuery.value
  activeQuery.value = appliedQuery.value
  searchEditing.value = false
}

function clearSearch() {
  selectionTouched.value = true
  pendingResourceSelection = null
  appliedQuery.value = ''
  draftQuery.value = ''
  activeQuery.value = ''
  searchEditing.value = false
}

let detachLaunchRequest: (() => void) | void
let detachCloseRequest: (() => void) | void

onMounted(async () => {
  window.addEventListener('focus', onFocus)
  detachLaunchRequest = window.runtime?.EventsOn?.(launchRequestEvent, onFocus)
  detachCloseRequest = window.runtime?.EventsOn?.(closeRequestEvent, () => {
    quitOpen.value = true
  })
  if (state.value) {
    if (activeQuery.value !== '') {
      state.value.setDiscoveryQuery(activeQuery.value)
    }
    void state.value.loadConfiguration()
    void state.value.checkRelease()
    return
  }
  try {
    const bootstrap = await resolveBootstrap()
    const c = new WorkspaceController(createClient(bootstrap.url, bootstrap.credential), undefined, nativeQuit())
    state.value = c
    if (activeQuery.value !== '') {
      c.setDiscoveryQuery(activeQuery.value)
    }
    void c.loadConfiguration()
    void c.checkRelease()
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
  detachCloseRequest?.()
  detachCloseRequest = undefined
  state.value?.dispose()
})

const view = computed(() => state.value?.state)
const resourceActionOpen = computed({
  get: () => view.value?.resourceAction != null,
  set: (open: boolean) => {
    if (!open) {
      state.value?.dismissResourceAction()
    }
  },
})

function rowKey(id: string): string {
  return `row:${id}`
}

function sectionKey(id: string): string {
  return `section:${id}`
}

function rowElementId(id: string): string {
  return `row-${id}`
}

function sectionElementId(id: string): string {
  return `section-${id}`
}

const searchMatches = computed<SearchMatch[]>(() => rankRows(view.value?.rows ?? [], activeQuery.value))
const searchMatchesByID = computed(() => new Map<string, SearchMatch>(searchMatches.value.map((match) => [match.row.id, match])))
const filteredSearch = computed(() => hasSearchTerms(activeQuery.value))
const localMatchCount = computed(() => searchMatches.value.filter((match) => !match.row.discovery).length)
const discoveryFailed = computed(() => view.value?.discoveryError != null || (view.value?.discoveryHealth ?? []).some((health) => health.message !== ''))

function nameParts(row: Row): NameSegment[] {
  const match = searchMatchesByID.value.get(row.id)
  return highlightNameParts(row.displayName, row.searchName, match?.nameMatch === true ? match.indexes : [])
}

const groups = computed<GroupView[]>(() => {
  const rowsByGroup = new Map<string, Row[]>()
  for (const match of searchMatches.value) {
    const groupRows = rowsByGroup.get(match.row.group) ?? []
    groupRows.push(match.row)
    rowsByGroup.set(match.row.group, groupRows)
  }
  return (view.value?.groups ?? []).flatMap((group) => {
    const rows = rowsByGroup.get(group.id) ?? []
    if (rows.length === 0) {
      return []
    }
    if (!filteredSearch.value) {
      rows.sort(compareRows)
    }
    const collapsed = filteredSearch.value
      ? temporarySearchFolds.value.query === activeQuery.value && temporarySearchFolds.value.groups[group.id] === true
      : foldedGroups.value[group.id] === true
    return [{ ...group, rows, collapsed }]
  })
})

const visibleEntries = computed<VisibleEntry[]>(() => {
  const entries: VisibleEntry[] = []
  for (const group of groups.value) {
    entries.push({ key: sectionKey(group.id), kind: 'section', elementId: sectionElementId(group.id) })
    if (!group.collapsed) {
      for (const row of group.rows) {
        entries.push({ key: rowKey(row.id), kind: 'row', elementId: rowElementId(row.id), row })
      }
    }
  }
  return entries
})

const selected = computed(() => {
  const key = selectedKey.value
  if (key == null || !key.startsWith('row:')) {
    return null
  }
  const rowID = key.slice('row:'.length)
  return (view.value?.rows ?? []).find((row) => row.id === rowID) ?? null
})

watch(
  () => selected.value?.id,
  (rowID) => {
    const controller = state.value
    const inspection = controller?.state.inspection
    if (inspection != null && inspection.rowId !== rowID) {
      controller?.closeInspection()
      inspectionOpen.value = false
    }
    const resourceAction = controller?.state.resourceAction
    if (resourceAction != null && resourceAction.rowId !== rowID && !controller?.resourceActionSubmitting) {
      pendingResourceSelection = null
      resourceActionFocusPending = false
      controller?.cancelResourceAction()
    }
  },
)

watch(
  () => state.value?.state.inspection,
  (inspection) => {
    if (inspection == null && inspectionOpen.value) {
      inspectionOpen.value = false
    }
  },
)

function nextSelectionKey(currentKey: string | null, previous: VisibleEntry[], next: VisibleEntry[]): string | null {
  const nextKeys = new Set(next.map((entry) => entry.key))
  if (currentKey == null) {
    return next.find((entry) => entry.kind === 'row')?.key ?? null
  }
  if (nextKeys.has(currentKey)) {
    return currentKey
  }

  const previousIndex = previous.findIndex((entry) => entry.key === currentKey)
  if (previousIndex >= 0) {
    const rowSelection = currentKey.startsWith('row:')
    for (let index = previousIndex + 1; index < previous.length; index++) {
      const candidate = previous[index]
      if (nextKeys.has(candidate.key) && (!rowSelection || candidate.kind === 'row')) {
        return candidate.key
      }
    }
    for (let index = previousIndex - 1; index >= 0; index--) {
      const candidate = previous[index]
      if (nextKeys.has(candidate.key) && (!rowSelection || candidate.kind === 'row')) {
        return candidate.key
      }
    }
  }
  return next.find((entry) => entry.kind === 'row')?.key ?? next[0]?.key ?? null
}

function nextRowAfterDisabled(rowId: string, previous: VisibleEntry[], next: VisibleEntry[]): string | null {
  const disabledKey = rowKey(rowId)
  const nextKeys = new Set(next.map((entry) => entry.key))
  const previousIndex = previous.findIndex((entry) => entry.key === disabledKey)
  if (previousIndex >= 0) {
    for (let index = previousIndex + 1; index < previous.length; index++) {
      const candidate = previous[index]
      if (candidate.kind === 'row' && candidate.key !== disabledKey && nextKeys.has(candidate.key)) {
        return candidate.key
      }
    }
    for (let index = previousIndex - 1; index >= 0; index--) {
      const candidate = previous[index]
      if (candidate.kind === 'row' && candidate.key !== disabledKey && nextKeys.has(candidate.key)) {
        return candidate.key
      }
    }
  }
  return next.find((entry) => entry.kind === 'row' && entry.key !== disabledKey)?.key ?? null
}

let previousVisibleEntries: VisibleEntry[] = []
let rowReplacementEpoch = 0
let pendingResourceSelection: PendingResourceSelection | null = null
let resourceActionFocusPending = false

watch(
  visibleEntries,
  (entries) => {
    previousVisibleEntries = entries
  },
  { flush: 'post' },
)

watch(
  activeQuery,
  () => {
    temporarySearchFolds.value = { query: activeQuery.value, groups: {} }
    if (filteredSearch.value) {
      const best = searchMatches.value[0]
      selectedKey.value = best == null ? null : rowKey(best.row.id)
    } else {
      selectedKey.value = nextSelectionKey(selectedKey.value, previousVisibleEntries, visibleEntries.value)
    }
    state.value?.setDiscoveryQuery(activeQuery.value)
  },
  { flush: 'pre' },
)

watch(
  [() => view.value?.rows, () => view.value?.stage],
  ([rows, stage], previous) => {
    if (rows == null || stage == null) {
      return
    }
    const entries = visibleEntries.value
    const acceptedReplacement = rows !== previous?.[0]
    const epoch = acceptedReplacement ? ++rowReplacementEpoch : rowReplacementEpoch
    const nextKey = selectionTouched.value
      ? nextSelectionKey(selectedKey.value, previousVisibleEntries, entries)
      : entries.find((entry) => entry.kind === 'row')?.key ?? null
    const settlement = view.value?.settlementIntent ?? view.value?.settledMutation
    const currentResourceAction = view.value?.resourceAction
    const pendingDisable = settlement?.selectNext
      ? { rowId: settlement.rowId, operationId: settlement.operation.id }
      : currentResourceAction?.loading &&
          (currentResourceAction.plan?.action === 'remove' || currentResourceAction.plan?.action === 'exclude')
        ? { rowId: currentResourceAction.rowId, operationId: null }
        : null
    if (
      acceptedReplacement && stage === 'final' && view.value?.operation == null && selectionTouched.value && pendingDisable != null &&
      selectedKey.value === rowKey(pendingDisable.rowId)
    ) {
      pendingResourceSelection = {
        rowId: pendingDisable.rowId,
        selectedKey: rowKey(pendingDisable.rowId),
        operationId: pendingDisable.operationId,
        nextKey: nextRowAfterDisabled(pendingDisable.rowId, previousVisibleEntries, entries),
        previous: previousVisibleEntries.slice(),
      }
    }
    let anchor: ViewportAnchor | null = null
    if (acceptedReplacement && selectionTouched.value && nextKey != null) {
      const previousEntry = previousVisibleEntries.find((entry) => entry.key === nextKey)
      const root = listRoot.value
      const element = previousEntry == null ? null : document.getElementById(previousEntry.elementId)
      if (root != null && element != null) {
        anchor = {
          key: nextKey,
          top: element.getBoundingClientRect().top - root.getBoundingClientRect().top,
        }
      }
    }
    selectedKey.value = nextKey
    if (!acceptedReplacement) {
      return
    }
    const capturedKey = nextKey
    const selectionWasTouched = selectionTouched.value
    void nextTick(() => {
      if (epoch !== rowReplacementEpoch || selectedKey.value !== capturedKey || (!selectionWasTouched && selectionTouched.value)) {
        return
      }
      const root = listRoot.value
      if (root == null) {
        return
      }
      if (!selectionWasTouched) {
        root.scrollTop = 0
        return
      }
      if (anchor == null) {
        return
      }
      const entry = visibleEntries.value.find((candidate) => candidate.key === anchor?.key)
      const element = entry == null ? null : document.getElementById(entry.elementId)
      if (element == null) {
        return
      }
      const top = element.getBoundingClientRect().top - root.getBoundingClientRect().top
      root.scrollTop += top - anchor.top
    })
  },
  { flush: 'pre' },
)

function select(row: Row) {
  selectionTouched.value = true
  pendingResourceSelection = null
  const key = rowKey(row.id)
  if (selectedKey.value === key) {
    return
  }
  selectedKey.value = key
  state.value?.browse(row)
}

function openInspection(row: Row) {
  const controller = state.value
  if (controller == null || (!row.inspectable && !row.installedFiles)) {
    return
  }
  controller.openInspection(row.id)
  if (controller.state.inspection != null) {
    inspectionOpen.value = true
  }
}

function resourceActionLabel(action: Row['resourceAction']): string {
  switch (action) {
    case 'install':
      return 'Install Resource'
    case 'remove':
      return 'Remove Resource'
    case 'exclude':
      return 'Exclude Resource'
    case 'restore':
      return 'Restore Resource'
    default:
      return 'Resource action'
  }
}

function activateRow(row: Row) {
  select(row)
  if (row.inspectable || row.installedFiles) {
    openInspection(row)
  }
}

function toggleGroup(groupID: string) {
  selectionTouched.value = true
  pendingResourceSelection = null
  if (filteredSearch.value) {
    const groups = temporarySearchFolds.value.query === activeQuery.value
      ? temporarySearchFolds.value.groups
      : {}
    temporarySearchFolds.value = {
      query: activeQuery.value,
      groups: { ...groups, [groupID]: groups[groupID] !== true },
    }
  } else {
    foldedGroups.value = { ...foldedGroups.value, [groupID]: !foldedGroups.value[groupID] }
  }
  selectedKey.value = sectionKey(groupID)
}

async function moveSelection(direction: 1 | -1) {
  selectionTouched.value = true
  pendingResourceSelection = null
  const entries = visibleEntries.value
  if (entries.length === 0) {
    return
  }
  const index = entries.findIndex((entry) => entry.key === selectedKey.value)
  const nextIndex = index === -1 ? (direction > 0 ? 0 : entries.length - 1) : Math.min(entries.length - 1, Math.max(0, index + direction))
  const next = entries[nextIndex]
  const changed = selectedKey.value !== next.key
  selectedKey.value = next.key
  if (changed && next.kind === 'row') {
    state.value?.browse(next.row)
  }
  if (changed) {
    await nextTick()
    const element = document.getElementById(next.elementId)
    element?.focus()
    if (element != null && typeof element.scrollIntoView === 'function') {
      element.scrollIntoView({ block: 'nearest' })
    }
  }
}

const busy = computed(() => view.value == null || view.value.operation != null || view.value.requesting)
const ready = computed(() => view.value != null && eligible(view.value))

function openResourceAction(row: Row) {
  selectionTouched.value = true
  resourceActionFocusPending = true
  pendingResourceSelection = null
  state.value?.openResourceAction(row.id)
}

function tryRestoreResourceActionFocus() {
  if (
    !resourceActionFocusPending || resourceActionOpen.value || state.value?.resourceActionSubmitting ||
    view.value?.operation != null || view.value?.requesting || view.value?.settlementIntent != null || view.value?.settledMutation != null
  ) {
    return
  }
  void nextTick(() => {
    if (
      !resourceActionFocusPending || resourceActionOpen.value || state.value?.resourceActionSubmitting ||
      view.value?.operation != null || view.value?.requesting || view.value?.settlementIntent != null || view.value?.settledMutation != null
    ) {
      return
    }
    const actionButton = resourceActionButton.value
    if (actionButton != null && !actionButton.disabled) {
      actionButton.focus()
    } else if (selected.value != null) {
      document.getElementById(rowElementId(selected.value.id))?.focus()
    }
    resourceActionFocusPending = false
  })
}

watch(
  resourceActionOpen,
  (isOpen, wasOpen) => {
    if (!isOpen && wasOpen) {
      const current = view.value
      if (current?.operation == null && current?.settlementIntent == null && current?.settledMutation == null) {
        pendingResourceSelection = null
      }
      tryRestoreResourceActionFocus()
    }
  },
)

watch(
  () => view.value?.settlementIntent,
  (intent, previous) => {
    if (intent != null && pendingResourceSelection?.rowId === intent.rowId) {
      pendingResourceSelection = { ...pendingResourceSelection, operationId: intent.operation.id }
    } else if (intent == null && previous != null && view.value?.settledMutation?.operation.id !== previous.operation.id) {
      if (pendingResourceSelection?.rowId === previous.rowId && pendingResourceSelection.operationId === previous.operation.id) {
        pendingResourceSelection = null
      }
    }
  },
)

watch(
  () => view.value?.settledMutation,
  (settled) => {
    if (settled == null) {
      return
    }
    const pending = pendingResourceSelection
    const settledRow = view.value?.rows.find((row) => row.id === settled.rowId)
    const disabled = settledRow == null || !settledRow.checked
    if (
      settled.selectNext && disabled && pending != null && pending.rowId === settled.rowId &&
      (pending.operationId == null || pending.operationId === settled.operation.id) && selectedKey.value === pending.selectedKey
    ) {
      const visibleKeys = new Set(visibleEntries.value.map((entry) => entry.key))
      const target = pending.nextKey != null && visibleKeys.has(pending.nextKey)
        ? pending.nextKey
        : nextRowAfterDisabled(pending.rowId, pending.previous, visibleEntries.value)
      selectedKey.value = target
    }
    if (pending?.rowId === settled.rowId && (pending.operationId == null || pending.operationId === settled.operation.id)) {
      pendingResourceSelection = null
    }
    state.value?.consumeSettledMutation(settled.operation.id)
    tryRestoreResourceActionFocus()
  },
  { flush: 'post' },
)

watch(
  [() => view.value?.operation, () => view.value?.settlementIntent, () => view.value?.settledMutation, ready],
  () => tryRestoreResourceActionFocus(),
)

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
        <button
          ref="searchButton"
          type="button"
          aria-label="Search"
          :aria-expanded="searchEditing"
          @click="openSearch"
        >Search</button>
        <input
          v-if="searchEditing"
          id="workspace-search"
          ref="searchInput"
          class="workspace__search-input"
          type="search"
          aria-label="Search resources"
          :value="draftQuery"
          @input="updateSearch"
          @keydown.enter.prevent="confirmSearch"
          @keydown.esc.prevent="cancelSearch"
        />
        <span v-if="activeQuery !== ''" class="workspace__search-status" role="status" aria-live="polite">
          {{ activeQuery }} · {{ localMatchCount }} local {{ localMatchCount === 1 ? 'match' : 'matches' }}
        </span>
        <button
          v-if="activeQuery !== '' || appliedQuery !== '' || draftQuery !== ''"
          type="button"
          aria-label="Clear search"
          @click="clearSearch"
        >Clear search</button>
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
        <button
          type="button"
          aria-label="Quit"
          @click="quitOpen = true"
        >Quit</button>
      </div>
    </header>

    <p v-if="error || view?.error" class="workspace__error" role="alert">{{ error ?? view?.error }}</p>
    <p v-else-if="view && view.stage !== '' && view.harnesses.length === 0" class="workspace__notice" role="status">
      No supported target harnesses are configured.
    </p>
    <p v-else-if="view?.operation" class="workspace__notice" role="status">
      {{ view.operation.kind }} {{ view.phase ?? 'running' }}
    </p>
    <p v-if="view?.latestVersion" class="workspace__notice" role="status">
      Agent Marketplace {{ view.latestVersion }} is available.
    </p>

    <div class="workspace__panes">
      <section ref="listRoot" aria-label="Resources" class="workspace__list">
        <div
          v-for="group in groups"
          :key="group.id"
          class="workspace__group"
        >
          <h2>
            <button
              :id="sectionElementId(group.id)"
              type="button"
              :class="{ 'workspace__group-toggle--selected': selectedKey === sectionKey(group.id) }"
              class="workspace__group-toggle"
              :aria-label="group.title"
              :aria-expanded="!group.collapsed"
              :aria-current="selectedKey === sectionKey(group.id) ? 'true' : undefined"
              @click="toggleGroup(group.id)"
              @keydown.down.prevent="moveSelection(1)"
              @keydown.up.prevent="moveSelection(-1)"
            >
              <span>{{ group.title }}</span>
              <span aria-hidden="true">{{ group.collapsed ? '▸' : '▾' }}</span>
            </button>
          </h2>
          <ul v-if="!group.collapsed" role="listbox" :aria-label="'Resources: ' + group.title">
            <li
              v-for="row in group.rows"
              :id="rowElementId(row.id)"
              :key="row.id"
              role="option"
              tabindex="0"
              :aria-selected="selectedKey === rowKey(row.id)"
              :class="{ 'workspace__row--selected': selectedKey === rowKey(row.id) }"
              class="workspace__row"
              @click="select(row)"
              @keydown.enter.prevent="activateRow(row)"
              @keydown.space.prevent="select(row)"
              @keydown.down.prevent="moveSelection(1)"
              @keydown.up.prevent="moveSelection(-1)"
            >
              <span class="workspace__check" aria-hidden="true">{{ row.checked ? '[x]' : '[ ]' }}</span>
              <span class="workspace__name">
                <span>{{ row.registry }}/{{ row.kind }}/</span><template v-for="(part, index) in nameParts(row)" :key="index">
                  <mark v-if="part.matched" class="workspace__name-match">{{ part.text }}</mark>
                  <template v-else>{{ part.text }}</template>
                </template>
              </span>
              <span class="workspace__scope">{{ row.scope }}</span>
              <span v-if="row.harnessOverride" class="workspace__badge" title="Registry-level harness override">harnesses: {{ row.harnesses.join(', ') }}</span>
            </li>
          </ul>
        </div>
      </section>

      <aside aria-label="Detail" class="workspace__detail">
        <template v-if="selected">
          <h2>{{ selected.registry }}/{{ selected.kind }}/{{ selected.displayName }}</h2>
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
          <button
            v-if="selected.resourceAction !== 'none'"
            ref="resourceActionButton"
            type="button"
            :aria-label="resourceActionLabel(selected.resourceAction)"
            class="workspace__open-resource"
            :disabled="!ready || configuration?.editable !== true || busy || resourceActionOpen"
            @click="openResourceAction(selected)"
          >{{ resourceActionLabel(selected.resourceAction) }}</button>
          <button
            v-if="selected.inspectable || selected.installedFiles"
            ref="openResourceButton"
            type="button"
            aria-label="Open resource"
            class="workspace__open-resource"
            @click="openInspection(selected)"
          >Open resource</button>
        </template>
        <p v-else>Select a resource to inspect it.</p>

        <h2>Session</h2>
        <p>Drift: {{ view?.drift }}</p>
        <p>Harnesses: <span class="mono">{{ (view?.harnesses ?? []).join(', ') || 'none' }}</span></p>
        <section v-if="view?.discoveryEnabled" role="region" aria-label="Marketplace discovery health" class="workspace__discovery-health">
          <h2>Marketplace discovery</h2>
          <p v-if="view.discoveryLoading" role="status" aria-live="polite">Marketplace discovery is updating.</p>
          <p v-if="view.discoveryError" role="status">{{ view.discoveryError }}</p>
          <button v-if="discoveryFailed" type="button" aria-label="Retry Marketplace search" @click="state?.retryDiscovery()">Retry Marketplace search</button>
          <ul v-if="view.discoveryHealth.length" aria-label="Marketplace endpoint health">
            <li v-for="health in view.discoveryHealth" :key="health.id">
              <p><strong>{{ health.label }}</strong> · {{ health.cacheState }} · {{ health.total }} {{ health.total === 1 ? 'result' : 'results' }}<span v-if="health.truncated"> · truncated</span></p>
              <p v-if="health.message">{{ health.message }}</p>
            </li>
          </ul>
        </section>

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
    <Confirmations v-if="state" v-model:quit-open="quitOpen" :controller="state" />
    <ResourceActions v-if="state" v-model:open="resourceActionOpen" :controller="state" />
    <ResourceInspection v-if="state" v-model:open="inspectionOpen" :controller="state" />

    <ul class="workspace__notifications" aria-live="polite" aria-label="Notifications">
      <li v-for="n in view?.notifications" :key="n.id" :class="`notification--${n.severity}`">
        {{ n.message }}
      </li>
    </ul>
  </div>
</template>
