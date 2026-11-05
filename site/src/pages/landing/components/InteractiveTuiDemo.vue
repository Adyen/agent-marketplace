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
  if (!hasFocus.value) return

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
    class="demo is-wide"
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
        class="focus-control am-hit-target"
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
          <div v-for="[registry, items] in groups" :key="registry">
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
.focus-control {
  position: relative;
  isolation: isolate;
  height: var(--lp-h-depiction);
  padding: 0 var(--lp-space-related);
  border: 0;
  border-radius: var(--lp-radius-control);
  background: var(--lp-fill);
  color: var(--lp-text);
  font: inherit;
  text-transform: uppercase;
  white-space: nowrap;
  cursor: pointer;
  transition: background-color var(--lp-motion-feedback) var(--lp-ease);
}

.focus-control::before {
  position: absolute;
  z-index: -1;
  inset: 0;
  border-radius: inherit;
  background: var(--lp-surface);
  content: "";
  opacity: 0;
  transition: opacity var(--lp-motion-feedback) var(--lp-ease);
}

.focus-control:hover::before {
  opacity: 1;
}

.focus-control[aria-pressed="true"] {
  background: var(--lp-green);
  color: var(--lp-on-green);
}

.demo.is-focused {
  background: var(--lp-focus-fill);
}

.target-list {
  display: flex;
  flex-wrap: wrap;
}

.target-name {
  white-space: nowrap;
}

.target-separator {
  margin: 0 var(--lp-space-tight);
  color: var(--lp-text-muted);
}

.spinner {
  display: inline-block;
  width: 1ch;
}

@media (prefers-reduced-motion: reduce) {
  .focus-control,
  .focus-control::before {
    transition: none;
  }
}
</style>
