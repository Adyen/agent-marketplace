<script setup lang="ts">
import { DEMO_LOCAL_REGISTRY, DEMO_PROJECT_NAME } from '../data/demo'
import TerminalFrame from './TerminalFrame.vue'

const registries = [
  {
    label: 'github.com · anthropics/skills',
    resources: [
      { kind: 'skill', name: 'pdf', scope: 'P', enabled: true, selected: true },
      { kind: 'skill', name: 'pptx', scope: 'G', enabled: false },
    ],
  },
  {
    label: DEMO_LOCAL_REGISTRY,
    resources: [
      { kind: 'skill', name: 'conventions', scope: 'L', enabled: true },
    ],
  },
]
</script>

<template>
  <TerminalFrame
    tag="figure"
    label="agent-marketplace tui"
    aria-label="Agent Marketplace terminal UI showing resources grouped by registry and their configured harnesses"
  >
    <template #status>
      <span class="preview-status am-outline-chip">Preview</span>
    </template>

    <div class="screen">
      <div class="title">agent-marketplace - {{ DEMO_PROJECT_NAME }}</div>

      <div class="panes">
        <div class="resources">
          <div v-for="registry in registries" :key="registry.label" class="registry">
            <div class="registry-title" :title="registry.label">
              <span>▼ {{ registry.label }}</span>
            </div>
            <div
              v-for="resource in registry.resources"
              :key="`${registry.label}:${resource.name}`"
              class="resource"
              :class="{ selected: resource.selected }"
            >
              <span class="check">{{ resource.enabled ? '[*]' : '[ ]' }}</span>
              <span class="kind">{{ resource.kind }}</span>
              <span class="name">{{ resource.name }}</span>
              <span class="scope">{{ resource.scope }}</span>
            </div>
          </div>
        </div>

        <aside class="details">
          <dl>
            <dt>Kind</dt>
            <dd>skill</dd>
            <dt>Name</dt>
            <dd>pdf</dd>
            <dt>Targets</dt>
            <dd>Claude Code · Codex · Devin</dd>
            <dt>Status</dt>
            <dd class="pending">enabled (pending sync)</dd>
          </dl>
        </aside>
      </div>

      <div class="activity">
        <span class="sync-status">~ 1 out of sync</span>
        <span><kbd>space</kbd> toggle · <kbd>s</kbd> sync</span>
      </div>
    </div>
  </TerminalFrame>
</template>
