<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { createClient } from './api/client'
import { closeRequestEvent, launchRequestEvent, nativeQuit, resolveBootstrap } from './bootstrap'
import { eligible, WorkspaceController, type Row } from './controller'
import { buildSections, readViewSettings, sectionPathsContaining, writeViewSettings, type Section, type ViewSettings } from './grouping'
import { hasSearchTerms, highlightNameParts, rankRows, type NameSegment, type SearchMatch } from './search'
import { readStoredValue, writeStoredValue } from './storage'
import Confirmations from './Confirmations.vue'
import FirstRun from './FirstRun.vue'
import GuiIcon from './GuiIcon.vue'
import HarnessSettings from './HarnessSettings.vue'
import Inspector from './Inspector.vue'
import MainToolbar from './MainToolbar.vue'
import ResourceActions from './ResourceActions.vue'
import ResourceInspection from './ResourceInspection.vue'
import ResourceList, { rowElementId, rowKey, sectionElementId, sectionKey } from './ResourceList.vue'
import Sidebar, { type SidebarGroup, type SidebarSource } from './Sidebar.vue'
import SourcesSettings from './SourcesSettings.vue'
import StatusBar from './StatusBar.vue'

type SectionView = Omit<Section, 'children'> & { collapsed: boolean; children: SectionView[] }
type VisibleEntry =
  | { key: string; kind: 'section'; elementId: string }
  | { key: string; kind: 'row'; elementId: string; row: Row }
type ViewportAnchor = { key: string; top: number }
type SearchFoldState = { query: string; groups: Record<string, boolean> }
type PendingResourceSelection = { rowId: string; selectedKey: string; operationId: number | null; nextKey: string | null; previous: VisibleEntry[] }
type ElementHost = { $el: HTMLElement } | null

const props = defineProps<{ controller?: WorkspaceController }>()

const state = shallowRef<WorkspaceController | null>(props.controller ?? null)
const selectedKey = ref<string | null>(null)
const selectionTouched = ref(false)
const viewSettings = ref<ViewSettings>(readViewSettings())
const foldedGroups = ref<Record<string, Record<string, boolean>>>({})
const temporarySearchFolds = ref<SearchFoldState>({ query: '', groups: {} })
const activeQuery = ref('')
const draftQuery = ref('')
const appliedQuery = ref('')
const searchEditing = ref(false)
const error = ref<string | null>(null)
const settingsOpen = ref(false)
const sourcesOpen = ref(false)
const setupOpen = ref(false)
const setupDismissed = ref(false)
const setupOpened = ref(false)
const inspectionOpen = ref(false)
const quitOpen = ref(false)
const sidebar = ref<InstanceType<typeof Sidebar> | null>(null)
const toolbar = ref<InstanceType<typeof MainToolbar> | null>(null)
const inspector = ref<InstanceType<typeof Inspector> | null>(null)
const resourceList = ref<ElementHost>(null)

const storageKeys = { sidebar: 'am-gui-sidebar', inspector: 'am-gui-inspector' }
const sidebarOverlayWidth = 720
const inspectorOverlayWidth = 1000
const viewportWidth = ref(window.innerWidth)

function readPanelChoice(key: string): boolean | null {
  const value = readStoredValue(key)
  return value === 'folded' ? true : value === 'open' ? false : null
}

function writePanelChoice(key: string, folded: boolean) {
  writeStoredValue(key, folded ? 'folded' : 'open')
}

const sidebarChoice = ref<boolean | null>(readPanelChoice(storageKeys.sidebar))
const inspectorChoice = ref<boolean | null>(readPanelChoice(storageKeys.inspector))
const sidebarOverlay = computed(() => viewportWidth.value < sidebarOverlayWidth)
const inspectorOverlay = computed(() => viewportWidth.value < inspectorOverlayWidth)
const sidebarFolded = computed(() => sidebarChoice.value ?? sidebarOverlay.value)
const inspectorFolded = computed(() => inspectorChoice.value ?? inspectorOverlay.value)
const sidebarOverlayOpen = computed(() => sidebarOverlay.value && !sidebarFolded.value)
const inspectorOverlayOpen = computed(() => inspectorOverlay.value && !inspectorFolded.value)
const shellColumns = computed(() => {
  const sidebarColumn = sidebarFolded.value || sidebarOverlay.value ? '0px' : 'var(--gui-sidebar-width)'
  const inspectorColumn = inspectorFolded.value || inspectorOverlay.value ? '0px' : 'var(--gui-inspector-width)'
  return `${sidebarColumn} minmax(0, 1fr) ${inspectorColumn}`
})

const configuration = computed(() => state.value?.state.configuration ?? null)

watch(searchEditing, (isEditing, wasEditing) => {
  if (!isEditing && wasEditing) {
    void nextTick(() => toolbar.value?.focusSearchButton())
  }
})

watch(settingsOpen, (isOpen, wasOpen) => {
  if (!isOpen && wasOpen && !sourcesOpen.value) {
    void nextTick(() => sidebar.value?.focusSettings())
  }
})

function openSourcesFromSettings() {
  settingsOpen.value = false
  sourcesOpen.value = true
}

watch(sourcesOpen, (isOpen, wasOpen) => {
  if (!isOpen && wasOpen) {
    void nextTick(() => sidebar.value?.focusSources())
  }
})

watch(inspectionOpen, (isOpen, wasOpen) => {
  if (!isOpen && wasOpen) {
    void nextTick(() => inspector.value?.focusOpenResource())
  }
})

watch(setupOpen, (isOpen, wasOpen) => {
  if (isOpen) {
    setupOpened.value = true
    return
  }
  if (wasOpen) {
    void nextTick(() => {
      if (!toolbar.value?.focusSetup()) {
        sidebar.value?.focusSettings()
      }
    })
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
        void nextTick(() => sidebar.value?.focusSettings())
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

function updateSearch(query: string) {
  selectionTouched.value = true
  pendingResourceSelection = null
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

function listElement(): HTMLElement | null {
  return resourceList.value?.$el ?? null
}

function hasFocusWithin(element: unknown): boolean {
  return element instanceof HTMLElement && element.contains(document.activeElement)
}

function setSidebarFolded(folded: boolean) {
  const focusWasInside = hasFocusWithin(sidebar.value?.$el)
  sidebarChoice.value = folded
  writePanelChoice(storageKeys.sidebar, folded)
  if (folded && focusWasInside) {
    void nextTick(() => toolbar.value?.focusSidebarToggle())
  }
}

function setInspectorFolded(folded: boolean) {
  const focusWasInside = hasFocusWithin(inspector.value?.$el)
  inspectorChoice.value = folded
  writePanelChoice(storageKeys.inspector, folded)
  if (folded && focusWasInside) {
    void nextTick(() => toolbar.value?.focusInspectorToggle())
  }
}

function toggleSidebar() {
  const unfolding = sidebarFolded.value
  const focusOnToggle = document.activeElement?.closest('.gui-toolbar__leading') != null
  setSidebarFolded(!unfolding)
  if (unfolding && focusOnToggle) {
    void nextTick(() => sidebar.value?.focusToggle())
  }
}

function toggleInspector() {
  setInspectorFolded(!inspectorFolded.value)
}

function dismissOverlays() {
  if (sidebarOverlayOpen.value) {
    setSidebarFolded(true)
  }
  if (inspectorOverlayOpen.value) {
    setInspectorFolded(true)
  }
}

function showAll() {
  const list = listElement()
  if (list != null) {
    list.scrollTop = 0
  }
}

function selectGroup(groupID: string) {
  const group = sectionViews.value.find((candidate) => candidate.path === groupID)
  if (group == null) {
    return
  }
  if (group.collapsed) {
    toggleGroup(groupID)
  } else {
    selectionTouched.value = true
    pendingResourceSelection = null
    selectedKey.value = sectionKey(groupID)
  }
  void nextTick(() => {
    const element = document.getElementById(sectionElementId(groupID))
    element?.scrollIntoView({ block: 'start' })
    element?.focus({ preventScroll: true })
  })
}

const navGroups = computed<SidebarGroup[]>(() =>
  sectionViews.value.map((section) => ({ id: section.path, title: section.title, count: section.count })),
)
const pendingCount = computed(() => (view.value?.rows ?? []).filter((row) => row.group === 'pending').length)
const rowSourceLabels = computed(() => new Map((view.value?.rows ?? []).map((row) => [row.source.id, row.source.label])))
const pendingSectionId = computed(() => (viewSettings.value.groupBy === 'status' ? 'pending' : 'attention'))
const sidebarSources = computed<SidebarSource[]>(() => {
  const config = configuration.value
  if (config == null) {
    return []
  }
  return [
    ...config.userSources.map((source) => ({ source, organization: false })),
    ...config.organizationSources.map((source) => ({ source, organization: true })),
  ]
})

function modalIsOpen(): boolean {
  return document.querySelector('[role="dialog"]') != null
}

function isTextEntry(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false
  }
  if (target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || target.isContentEditable) {
    return true
  }
  return target instanceof HTMLInputElement && !['checkbox', 'radio', 'button', 'submit', 'reset'].includes(target.type)
}

function onKeydown(event: KeyboardEvent) {
  if (event.defaultPrevented) {
    return
  }
  if (event.key === 'Escape') {
    if ((sidebarOverlayOpen.value || inspectorOverlayOpen.value) && !modalIsOpen()) {
      event.preventDefault()
      dismissOverlays()
    }
    return
  }
  if (modalIsOpen()) {
    return
  }
  if (event.metaKey) {
    if (event.code === 'KeyB') {
      event.preventDefault()
      if (event.altKey) {
        toggleInspector()
      } else {
        toggleSidebar()
      }
    } else if (event.altKey) {
      return
    } else if (event.key === 'k') {
      event.preventDefault()
      openSearch()
    } else if (event.key === ',') {
      event.preventDefault()
      settingsOpen.value = true
    } else if (event.code === 'KeyR' && !busy.value) {
      event.preventDefault()
      void refresh()
    }
    return
  }
  if (event.ctrlKey || event.altKey || isTextEntry(event.target)) {
    return
  }
  if (event.key === '/') {
    event.preventDefault()
    openSearch()
  } else if (event.key === 'v') {
    event.preventDefault()
    toolbar.value?.openViewMenu()
  } else if (event.key === 's' && ready.value && listElement()?.contains(document.activeElement) === true) {
    event.preventDefault()
    void run('sync')
  }
}

function onResize() {
  viewportWidth.value = window.innerWidth
}

let detachLaunchRequest: (() => void) | void
let detachCloseRequest: (() => void) | void

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', onResize)
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
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', onResize)
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

const searchMatches = computed<SearchMatch[]>(() => rankRows(view.value?.rows ?? [], activeQuery.value))
const searchMatchesByID = computed(() => new Map<string, SearchMatch>(searchMatches.value.map((match) => [match.row.id, match])))
const filteredSearch = computed(() => hasSearchTerms(activeQuery.value))
const localMatchCount = computed(() => searchMatches.value.filter((match) => !match.row.discovery).length)
const emptyState = computed<'resources' | 'search' | null>(() => {
  if (view.value?.stage !== 'final') {
    return null
  }
  if (filteredSearch.value) {
    return searchMatches.value.length === 0 ? 'search' : null
  }
  return (view.value.rows ?? []).length === 0 ? 'resources' : null
})
const discoveryFailed = computed(() => view.value?.discoveryError != null || (view.value?.discoveryHealth ?? []).some((health) => health.message !== ''))

function nameParts(row: Row): NameSegment[] {
  const match = searchMatchesByID.value.get(row.id)
  return highlightNameParts(row.displayName, row.searchName, match?.nameMatch === true ? match.indexes : [])
}

const foldKey = computed(() => `${viewSettings.value.groupBy}/${viewSettings.value.thenBy}`)

function isCollapsed(path: string): boolean {
  if (filteredSearch.value) {
    return temporarySearchFolds.value.query === activeQuery.value && temporarySearchFolds.value.groups[path] === true
  }
  return foldedGroups.value[foldKey.value]?.[path] === true
}

const sections = computed<Section[]>(() =>
  buildSections(searchMatches.value, view.value?.groups ?? [], viewSettings.value, filteredSearch.value),
)

const sectionViews = computed<SectionView[]>(() => {
  const toView = (section: Section): SectionView => ({
    ...section,
    collapsed: isCollapsed(section.path),
    children: section.children.map(toView),
  })
  return sections.value.map(toView)
})

const visibleEntries = computed<VisibleEntry[]>(() => {
  const entries: VisibleEntry[] = []
  const visit = (section: SectionView) => {
    entries.push({ key: sectionKey(section.path), kind: 'section', elementId: sectionElementId(section.path) })
    if (section.collapsed) {
      return
    }
    section.children.forEach(visit)
    for (const row of section.rows) {
      entries.push({ key: rowKey(row.id), kind: 'row', elementId: rowElementId(row.id), row })
    }
  }
  sectionViews.value.forEach(visit)
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
      const root = listElement()
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
      const root = listElement()
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

function toggleGroup(path: string) {
  selectionTouched.value = true
  pendingResourceSelection = null
  if (filteredSearch.value) {
    const groups = temporarySearchFolds.value.query === activeQuery.value
      ? temporarySearchFolds.value.groups
      : {}
    temporarySearchFolds.value = {
      query: activeQuery.value,
      groups: { ...groups, [path]: groups[path] !== true },
    }
  } else {
    const combination = foldedGroups.value[foldKey.value] ?? {}
    foldedGroups.value = {
      ...foldedGroups.value,
      [foldKey.value]: { ...combination, [path]: combination[path] !== true },
    }
  }
  selectedKey.value = sectionKey(path)
}

function selectionAfterRegrouping(key: string | null, entries: VisibleEntry[]): string | null {
  const visible = new Set(entries.map((entry) => entry.key))
  if (key != null && visible.has(key)) {
    return key
  }
  if (key != null && key.startsWith('row:')) {
    const enclosing = sectionPathsContaining(sections.value, key.slice('row:'.length)).map((path) => sectionKey(path))
    const deepest = enclosing.reverse().find((candidate) => visible.has(candidate))
    if (deepest != null) {
      return deepest
    }
  }
  return entries[0]?.key ?? null
}

function applyView(next: ViewSettings) {
  const current = viewSettings.value
  if (current.groupBy === next.groupBy && current.thenBy === next.thenBy) {
    return
  }
  selectionTouched.value = true
  pendingResourceSelection = null
  viewSettings.value = next
  writeViewSettings(next)
  temporarySearchFolds.value = { query: activeQuery.value, groups: {} }
  const entries = visibleEntries.value
  const previousKey = selectedKey.value
  selectedKey.value = selectionAfterRegrouping(previousKey, entries)
  const selectedEntry = entries.find((entry) => entry.key === selectedKey.value)
  if (selectedKey.value !== previousKey && selectedEntry?.kind === 'row') {
    state.value?.browse(selectedEntry.row)
  }
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
    if (!inspector.value?.focusResourceAction() && selected.value != null) {
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
  <div class="workspace gui-shell" :style="{ gridTemplateColumns: shellColumns }">
    <Sidebar
      ref="sidebar"
      :folded="sidebarFolded"
      :overlay="sidebarOverlay"
      :inert="sidebarFolded"
      :groups="navGroups"
      :pending-count="pendingCount"
      :pending-section-id="pendingSectionId"
      :sources="sidebarSources"
      :row-source-labels="rowSourceLabels"
      :harnesses="view?.harnesses ?? []"
      :latest-version="view?.latestVersion ?? ''"
      @toggle="toggleSidebar"
      @search="openSearch"
      @show-all="showAll"
      @select-group="selectGroup"
      @open-sources="sourcesOpen = true"
      @open-settings="settingsOpen = true"
      @quit="quitOpen = true"
    />

    <div class="workspace__main gui-main">
      <MainToolbar
        ref="toolbar"
        :sidebar-folded="sidebarFolded"
        :inspector-folded="inspectorFolded"
        :search-editing="searchEditing"
        :draft-query="draftQuery"
        :active-query="activeQuery"
        :applied-query="appliedQuery"
        :local-match-count="localMatchCount"
        :busy="busy"
        :ready="ready"
        :pending-count="pendingCount"
        :show-setup="configuration?.firstRun === true || setupOpen"
        :view-settings="viewSettings"
        @toggle-sidebar="toggleSidebar"
        @toggle-inspector="toggleInspector"
        @open-search="openSearch"
        @search-input="updateSearch"
        @confirm-search="confirmSearch"
        @cancel-search="cancelSearch"
        @clear-search="clearSearch"
        @change-view="applyView"
        @refresh="refresh"
        @sync="run('sync')"
        @update="run('update')"
        @setup="setupOpen = true"
      />

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

      <section v-if="emptyState === 'resources'" class="gui-empty" aria-label="No resources">
        <GuiIcon name="folder" />
        <h2 class="gui-empty__title">No resources yet</h2>
        <p class="gui-empty__body">Add a source to see the skills, commands, and other resources it provides.</p>
        <UButton
          label="Add a source"
          aria-label="Add a source"
          class="gui-button--secondary"
          color="neutral"
          variant="soft"
          @click="sourcesOpen = true"
        />
      </section>
      <p v-else-if="emptyState === 'search'" class="gui-empty gui-empty--muted">No resources match “{{ activeQuery }}”.</p>

      <ResourceList
        v-show="emptyState == null"
        ref="resourceList"
        :sections="sectionViews"
        :selected-key="selectedKey"
        :name-parts="nameParts"
        :view-settings="viewSettings"
        @toggle-group="toggleGroup"
        @select="select"
        @activate="activateRow"
        @move="moveSelection"
      />

      <StatusBar :pending-count="view == null ? null : pendingCount" />

      <ul class="workspace__notifications" aria-live="polite" aria-label="Notifications">
        <li v-for="n in view?.notifications" :key="n.id" :class="`notification--${n.severity}`">
          <GuiIcon v-if="n.severity === 'error'" name="error" class="gui-toast__icon" />
          {{ n.message }}
        </li>
      </ul>
    </div>

    <Inspector
      ref="inspector"
      :folded="inspectorFolded"
      :overlay="inspectorOverlay"
      :inert="inspectorFolded"
      :selected="selected"
      :drift="view?.drift ?? ''"
      :discovery-enabled="view?.discoveryEnabled === true"
      :discovery-loading="view?.discoveryLoading === true"
      :discovery-error="view?.discoveryError ?? null"
      :discovery-health="view?.discoveryHealth ?? []"
      :discovery-failed="discoveryFailed"
      :diagnostics="view?.diagnostics ?? []"
      :action-disabled="!ready || configuration?.editable !== true || busy || resourceActionOpen"
      @open-resource-action="openResourceAction"
      @open-inspection="openInspection"
      @retry-discovery="state?.retryDiscovery()"
    />

    <div
      v-if="sidebarOverlayOpen || inspectorOverlayOpen"
      class="gui-scrim"
      aria-hidden="true"
      @click="dismissOverlays"
    />

    <HarnessSettings v-if="state" v-model:open="settingsOpen" :controller="state" @manage-sources="openSourcesFromSettings" />
    <SourcesSettings v-if="state" v-model:open="sourcesOpen" :controller="state" />
    <FirstRun v-if="state" v-model:open="setupOpen" :controller="state" @cancel="onSetupCancel" />
    <Confirmations v-if="state" v-model:quit-open="quitOpen" :controller="state" />
    <ResourceActions v-if="state" v-model:open="resourceActionOpen" :controller="state" />
    <ResourceInspection v-if="state" v-model:open="inspectionOpen" :controller="state" />
  </div>
</template>
