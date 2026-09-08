<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import TerminalFrame from './TerminalFrame.vue'
import {
  DEMO_LOCAL_REGISTRY,
  DEMO_PROJECT_NAME,
  DEMO_REMOTE_REGISTRY,
  DEMO_RESOURCE_NAMES,
} from '../data/demo'

type Scope = 'user' | 'repo' | 'local'

/**
 * This marketing demo intentionally compresses the real TUI flow: Space
 * toggles the selected row immediately, and each scope shows a fixed example
 * target set. The documentation must explain the actual per-scope modal and
 * harness-resolution rules.
 */
type Resource = {
  id: string
  kind: 'skill' | 'command' | 'mcp'
  name: string
  registry: string
  scope: Scope
  enabled: boolean
}

const scopeMeta: Record<Scope, { label: string; marker: string; targets: string[] }> = {
  user: { label: 'User', marker: 'G', targets: ['Factory Droid', 'Windsurf'] },
  repo: { label: 'Repository', marker: 'P', targets: ['Claude Code', 'Cursor', 'Codex'] },
  local: { label: 'Local', marker: 'L', targets: ['Claude Code', 'Codex'] },
}

const initialResources: Resource[] = [
  {
    id: 'auth-preflight',
    kind: 'skill',
    name: 'auth-preflight-shared',
    registry: DEMO_REMOTE_REGISTRY,
    scope: 'repo',
    enabled: true,
  },
  {
    id: DEMO_RESOURCE_NAMES.oncall,
    kind: 'skill',
    name: DEMO_RESOURCE_NAMES.oncall,
    registry: DEMO_REMOTE_REGISTRY,
    scope: 'user',
    enabled: true,
  },
  {
    id: 'docs',
    kind: 'mcp',
    name: DEMO_RESOURCE_NAMES.docsSearch,
    registry: DEMO_LOCAL_REGISTRY,
    scope: 'repo',
    enabled: false,
  },
]

const cloneResources = () => initialResources.map((resource) => ({ ...resource }))

const resources = ref(cloneResources())
const selectedID = ref(initialResources[0].id)
const syncState = ref<'pending' | 'syncing' | 'synced'>('pending')
const spinner = ref('\\')
const terminal = ref<InstanceType<typeof TerminalFrame> | null>(null)
const hasFocus = ref(false)
let spinnerTimer: ReturnType<typeof setInterval> | undefined
let syncTimer: ReturnType<typeof setTimeout> | undefined

const selected = computed(
  () => resources.value.find((resource) => resource.id === selectedID.value) ?? resources.value[0],
)

const groups = computed(() => {
  const grouped = new Map<string, Resource[]>()
  for (const resource of resources.value) {
    const group = grouped.get(resource.registry) ?? []
    group.push(resource)
    grouped.set(resource.registry, group)
  }
  return [...grouped]
})

const enabledCount = computed(() => resources.value.filter((resource) => resource.enabled).length)

function toggleResource(id: string) {
  const resource = resources.value.find((item) => item.id === id)
  if (!resource) return

  resource.enabled = !resource.enabled
  selectedID.value = id
  markPending()
}

function sync() {
  if (syncState.value === 'syncing') return

  clearSyncTimers()
  syncState.value = 'syncing'
  const frames = ['\\', '|', '/', '—']
  let frame = 0
  spinner.value = frames[frame]
  spinnerTimer = setInterval(() => {
    frame = (frame + 1) % frames.length
    spinner.value = frames[frame]
  }, 120)
  syncTimer = setTimeout(() => {
    clearSyncTimers()
    syncState.value = 'synced'
  }, 680)
}

function reset() {
  resources.value = cloneResources()
  selectedID.value = initialResources[0].id
  markPending()
}

function clearSyncTimers() {
  if (spinnerTimer) clearInterval(spinnerTimer)
  if (syncTimer) clearTimeout(syncTimer)
  spinnerTimer = undefined
  syncTimer = undefined
}

function markPending() {
  clearSyncTimers()
  syncState.value = 'pending'
}

function focusDemo() {
  terminal.value?.focus()
}

function selectOffset(offset: number) {
  const currentIndex = resources.value.findIndex((resource) => resource.id === selectedID.value)
  const nextIndex = (currentIndex + offset + resources.value.length) % resources.value.length
  selectedID.value = resources.value[nextIndex].id
}

function handleKeydown(event: KeyboardEvent) {
  switch (event.key) {
    case 'ArrowDown':
      event.preventDefault()
      selectOffset(1)
      break
    case 'ArrowUp':
      event.preventDefault()
      selectOffset(-1)
      break
    case ' ':
      event.preventDefault()
      toggleResource(selectedID.value)
      break
    case 's':
    case 'S':
      event.preventDefault()
      sync()
      break
    case 'r':
    case 'R':
      event.preventDefault()
      reset()
      break
  }
}

onUnmounted(clearSyncTimers)
</script>

<template>
  <TerminalFrame
    ref="terminal"
    class="demo"
    :class="{ 'is-focused': hasFocus }"
    tag="figure"
    label="agent-marketplace tui"
    tabindex="0"
    aria-label="Keyboard-only Agent Marketplace TUI demo"
    @click="focusDemo"
    @focus="hasFocus = true"
    @blur="hasFocus = false"
    @keydown="handleKeydown"
  >
    <template #status>
      <button
        class="focus-control"
        type="button"
        :aria-pressed="hasFocus"
        @click.stop="focusDemo"
      >
        {{ hasFocus ? 'Keyboard active' : 'Click to interact' }}
      </button>
    </template>

    <div class="screen">
      <div class="title">agent-marketplace — {{ DEMO_PROJECT_NAME }}</div>

      <div class="panes">
        <div class="resources" aria-label="Available resources">
          <div v-for="[registry, items] in groups" :key="registry" class="registry">
            <div class="registry-title">▼ {{ registry }}</div>
            <div
              v-for="resource in items"
              :key="resource.id"
              class="resource"
              :class="{ selected: selected.id === resource.id }"
            >
              <span class="toggle">[{{ resource.enabled ? 'x' : ' ' }}]</span>
              <span class="resource-row">
                <span class="kind">{{ resource.kind }}</span>
                <span class="name">{{ resource.name }}</span>
                <span class="scope">{{ scopeMeta[resource.scope].marker }}</span>
              </span>
            </div>
          </div>
        </div>

        <aside class="details" aria-live="polite">
          <dl>
            <dt>Kind:</dt>
            <dd>{{ selected.kind }}</dd>
            <dt>Name:</dt>
            <dd>{{ selected.name }}</dd>
            <dt>Scope:</dt>
            <dd>{{ scopeMeta[selected.scope].label }}</dd>
            <dt>Targets:</dt>
            <dd class="target-list">
              <span
                v-for="(target, index) in scopeMeta[selected.scope].targets"
                :key="target"
                class="target-name"
              >
                {{ target }}<span
                  v-if="index < scopeMeta[selected.scope].targets.length - 1"
                  class="target-separator"
                  aria-hidden="true"
                >·</span>
              </span>
            </dd>
            <dt>Status:</dt>
            <dd
              :class="{
                pending: syncState === 'syncing' || (selected.enabled && syncState === 'pending'),
                positive: selected.enabled && syncState === 'synced',
              }"
            >
              {{
                selected.enabled
                  ? (syncState === 'synced' ? 'enabled' : syncState === 'syncing' ? 'syncing...' : 'enabled (pending sync)')
                  : 'disabled'
              }}
            </dd>
          </dl>
        </aside>
      </div>

      <div class="activity">
        <span class="sync-status" :class="{ positive: syncState === 'synced' }">
          <template v-if="syncState === 'syncing'">
            <span class="spinner" aria-hidden="true">{{ spinner }}</span> syncing...
          </template>
          <template v-else>
            {{ syncState === 'synced' ? '✓ in sync' : `~ ${enabledCount} enabled, pending sync` }}
          </template>
        </span>
        <span><kbd>↑↓</kbd> navigate · <kbd>space</kbd> toggle · <kbd>s</kbd> sync</span>
      </div>
    </div>
  </TerminalFrame>
</template>

<style scoped>
.demo {
  --tui-bg-raised: var(--am-terminal-bg-raised);
  --tui-border: var(--am-terminal-border);
  --tui-text: var(--am-terminal-text);
  --tui-muted: var(--am-terminal-muted);
  --tui-cyan: var(--am-terminal-cyan);
  --tui-green: var(--am-terminal-accent);
  --tui-orange: var(--am-terminal-warning);
  --tui-selection: var(--am-terminal-selection);
  --am-terminal-font-size: 14px;
  --am-terminal-line-height: 1.45;
}

.activity {
  align-items: center;
  gap: 12px;
}

.activity {
  display: flex;
  justify-content: space-between;
}

.focus-control {
  justify-self: end;
  padding: 3px 7px;
  border: 1px solid var(--tui-green);
  border-radius: var(--am-radius-sm);
  background: transparent;
  color: var(--tui-green);
  font: inherit;
  line-height: 1;
  cursor: pointer;
}

.focus-control:hover,
.is-focused .focus-control {
  background: var(--tui-selection);
}

.screen {
  padding: 16px 20px;
}

.title {
  margin-bottom: 9px;
  color: var(--tui-text);
  font-weight: 600;
}

.panes {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 18px;
  min-height: 226px;
}

.registry + .registry {
  margin-top: 9px;
}

.registry-title {
  margin-bottom: 2px;
  color: var(--tui-text);
  font-weight: 600;
  white-space: nowrap;
}

.resource {
  display: grid;
  grid-template-columns: 31px minmax(0, 1fr);
  min-height: 22px;
  padding: 0 4px;
}

.resource.selected {
  background: var(--tui-selection);
}

.toggle {
  color: var(--tui-green);
}

.resource-row {
  display: grid;
  grid-template-columns: 54px minmax(0, 1fr) 12px;
  min-width: 0;
}

.kind,
.scope {
  color: var(--tui-muted);
}

.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.details {
  min-width: 0;
  padding-left: 18px;
  border-left: 1px solid var(--tui-border);
}

.details dl {
  display: grid;
  grid-template-columns: 62px minmax(0, 1fr);
  gap: 3px 24px;
  margin: 0;
}

.details dt {
  color: var(--tui-cyan);
  font-weight: 600;
}

.details dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}

.target-list {
  display: flex;
  flex-wrap: wrap;
}

.target-name {
  white-space: nowrap;
}

.target-separator {
  margin: 0 6px;
  color: var(--tui-muted);
}

.pending,
.sync-status {
  color: var(--tui-orange);
}

.positive {
  color: var(--tui-green);
}

.spinner {
  display: inline-block;
  width: 1ch;
}

.activity {
  margin-top: 12px;
  padding-top: 9px;
  border-top: 1px solid var(--tui-border);
  font-size: 13px;
}

kbd {
  padding: 1px 4px;
  border: 1px solid var(--tui-border);
  border-radius: 3px;
  background: var(--tui-bg-raised);
  color: var(--tui-text);
  font: inherit;
}

.demo.is-focused {
  --am-terminal-border: var(--tui-green);
  --am-terminal-shadow:
<<<<<<< HEAD
    0 34px 72px -34px rgba(0, 0, 0, 0.92),
=======
    0 34px 72px -34px var(--am-terminal-shadow-strong),
>>>>>>> 389282c (Unify Marketplace styling and shared-origin routing)
    0 0 0 3px var(--vp-c-brand-soft),
    0 0 70px color-mix(in srgb, var(--am-color-action-active) 15%, transparent);
}

@media (max-width: 680px) {
  .demo {
    --am-terminal-font-size: 12.5px;
  }

  .panes {
    grid-template-columns: 1fr;
    min-height: auto;
  }

  .details {
    padding-top: 12px;
    padding-left: 0;
    border-top: 1px solid var(--tui-border);
    border-left: 0;
  }
}

</style>
