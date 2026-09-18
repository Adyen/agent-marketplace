<script setup lang="ts">
import SectionShell from "../components/SectionShell.vue";
import TerminalFrame from "../components/TerminalFrame.vue";
</script>

<template>
  <SectionShell
    id="safe-sync"
    eyebrow="Safe sync"
    title="Automate without touching handwritten config"
  >
    <template #sub>
      Use check mode in CI to detect drift before syncing managed entries.
    </template>

    <TerminalFrame
      label="CI check · read-only"
      status="exit 1"
      padded
      style="--am-terminal-status-color: var(--am-terminal-danger)"
    >
      <div class="command">
        <span class="prompt" aria-hidden="true">$</span>
        <code>agent-marketplace sync --check</code>
      </div>

      <div class="result" role="log" aria-label="Sync check output">
        <samp class="summary">
          sync: <strong>2 out of sync</strong> (1 missing, 1 orphaned)
        </samp>
        <samp class="root"
          >→ repo-agents (/workspace/payments-api/.agents)</samp
        >
        <samp class="change added">+ skills/pdf/SKILL.md</samp>
        <samp class="root">→ repo-codex (/workspace/payments-api/.codex)</samp>
        <samp class="change removed">- agents/old-agent.toml</samp>
        <samp class="error">Error: 2 skills out of sync</samp>
      </div>
    </TerminalFrame>

    <ul class="guarantees" aria-label="Sync guarantees">
      <li>Read-only CI checks</li>
      <li>Managed entries only</li>
      <li>Idempotent sync</li>
    </ul>
  </SectionShell>
</template>

<style scoped>
.command {
  display: flex;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--am-terminal-border);
  color: var(--am-terminal-text);
  font-size: 16px;
}

.prompt {
  margin-right: 10px;
  color: var(--am-terminal-accent);
}

.result {
  display: grid;
  gap: 2px;
  padding-top: 18px;
  color: var(--am-terminal-muted);
  font-size: 14px;
}

.result samp {
  overflow-x: auto;
  font: inherit;
  white-space: nowrap;
}

.summary {
  margin-bottom: 7px;
  color: var(--am-terminal-text);
}

.summary strong {
  color: var(--am-terminal-warning);
}

.error,
.removed {
  color: var(--am-terminal-danger);
}

.root {
  color: var(--am-terminal-cyan);
}

.change {
  padding-left: 24px;
}

.added {
  color: var(--am-terminal-accent);
}

.error {
  margin-top: 7px;
}

.guarantees {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 28px;
  margin: 24px 0 0;
  padding: 0;
  list-style: none;
}

.guarantees li {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--vp-c-text-1);
  font-size: 14px;
  font-weight: 600;
}

.guarantees li::before {
  color: var(--vp-c-brand-1);
  content: "✓";
  font-family: var(--vp-font-family-mono);
}
</style>
