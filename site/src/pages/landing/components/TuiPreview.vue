<script setup lang="ts">
import { DEMO_LOCAL_REGISTRY, DEMO_PROJECT_NAME } from "../data/demo";
import TerminalFrame from "./TerminalFrame.vue";

const registries = [
  {
    label: "github.com · anthropics/skills",
    resources: [
      { kind: "skill", name: "pdf", scope: "P", enabled: true, selected: true },
      { kind: "skill", name: "pptx", scope: "G", enabled: false },
    ],
  },
  {
    label: DEMO_LOCAL_REGISTRY,
    resources: [
      { kind: "skill", name: "conventions", scope: "L", enabled: true },
    ],
  },
];
</script>

<template>
  <TerminalFrame
    class="terminal"
    tag="figure"
    label="agent-marketplace tui"
    aria-label="Agent Marketplace terminal UI showing resources grouped by registry and their configured harnesses"
  >
    <template #status>
      <span class="ready"><i aria-hidden="true"></i> ready</span>
    </template>

    <div class="screen">
      <div class="title">agent-marketplace - {{ DEMO_PROJECT_NAME }}</div>

      <div class="panes">
        <div class="resources">
          <div
            v-for="registry in registries"
            :key="registry.label"
            class="registry"
          >
            <div class="registry-title" :title="registry.label">
              <span>▼ {{ registry.label }}</span>
            </div>
            <div
              v-for="resource in registry.resources"
              :key="`${registry.label}:${resource.name}`"
              class="resource"
              :class="{ selected: resource.selected }"
            >
              <span class="check">{{ resource.enabled ? "[*]" : "[ ]" }}</span>
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

<style scoped>
.terminal {
  --tui-bg-raised: var(--am-terminal-bg-raised);
  --tui-border: var(--am-terminal-border);
  --tui-text: var(--am-terminal-text);
  --tui-muted: var(--am-terminal-muted);
  --tui-cyan: var(--am-terminal-cyan);
  --tui-green: var(--am-terminal-accent);
  --tui-orange: var(--am-terminal-warning);
  --tui-selection: var(--am-terminal-selection);
  --am-terminal-font-size: 13.5px;
}

.activity {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.ready {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--tui-green);
}

.ready i {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: currentColor;
}

.pending,
.sync-status {
  color: var(--tui-orange);
}

.screen {
  padding: 16px 20px 18px;
}

.title {
  margin-bottom: 9px;
  color: var(--tui-text);
}

.panes {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.85fr);
  gap: 18px;
  min-height: 198px;
}

.resources,
.registry {
  min-width: 0;
}

.registry + .registry {
  margin-top: 8px;
}

.registry-title {
  min-width: 0;
  overflow: hidden;
  color: var(--tui-text);
  font-weight: 600;
}

.registry-title span {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kind,
.scope {
  color: var(--tui-muted);
}

.resource {
  display: grid;
  grid-template-columns: 28px 54px minmax(0, 1fr) 12px;
  min-height: 21px;
  padding: 0 4px;
}

.resource.selected {
  background: var(--tui-selection);
}

.check {
  color: var(--tui-green);
}

.resource:not(.selected) .check {
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
  grid-template-columns: 54px minmax(0, 1fr);
  gap: 4px 8px;
  margin: 0;
}

.details dt {
  color: var(--tui-cyan);
  font-weight: 600;
}

.details dd {
  min-width: 0;
  margin: 0;
}

.activity {
  margin-top: 12px;
  padding-top: 9px;
  border-top: 1px solid var(--tui-border);
  color: var(--tui-muted);
  font-size: 11.5px;
}

kbd {
  padding: 1px 4px;
  border: 1px solid var(--tui-border);
  border-radius: 3px;
  background: var(--tui-bg-raised);
  color: var(--tui-text);
  font: inherit;
}

@media (max-width: 680px) {
  .terminal {
    --am-terminal-font-size: 12px;
  }

  .panes {
    grid-template-columns: 1fr;
    min-height: auto;
  }

  .details {
    display: none;
  }
}
</style>
