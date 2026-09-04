<script setup lang="ts">
import CodeBlock from '../components/CodeBlock.vue'
import SectionShell from '../components/SectionShell.vue'
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

    <CodeBlock class="terminal" label="CI check · read-only">
      <div class="command">
        <span class="prompt" aria-hidden="true">$</span>
        <code>agent-marketplace sync --check</code>
      </div>

      <div class="result">
        <code>sync: <strong>2 out of sync</strong> (1 missing, 1 orphaned)</code>
        <pre>  .claude
    + skills/pdf/SKILL.md
  .codex
    - agents/old-agent.toml
Run '<strong class="reconcile-command">agent-marketplace sync</strong>' to reconcile.</pre>
      </div>
    </CodeBlock>

    <ul class="guarantees" aria-label="Sync guarantees">
      <li>Read-only CI checks</li>
      <li>Managed entries only</li>
      <li>Idempotent sync</li>
    </ul>
  </SectionShell>
</template>

<style scoped>
.terminal {
  --am-technical-font-size: 15px;
  --am-technical-padding: 22px;
  width: 100%;
}

.command {
  display: flex;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--am-code-border);
  color: var(--am-code-text);
  font-size: 16px;
}

.prompt {
  margin-right: 10px;
  color: var(--vp-c-brand-1);
}

.result {
  padding-top: 18px;
  color: var(--am-code-muted);
  font-size: 14px;
}

.result strong {
  color: var(--am-danger);
}

.result .reconcile-command {
  color: inherit;
  font-weight: 700;
}

.result pre {
  margin: 6px 0 0;
  overflow-x: auto;
  color: var(--am-code-muted);
  font: inherit;
  line-height: 1.65;
  white-space: pre;
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

@media (max-width: 680px) {
  .terminal {
    --am-technical-padding: 16px;
  }
}
</style>
