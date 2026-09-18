<script setup lang="ts">
import { targets } from "../data/harnesses";

const displayNames: Record<string, string> = {
  factory: "Factory",
  claude: "Claude Code",
  cursor: "Cursor",
  windsurf: "Windsurf",
  gemini: "Gemini",
  devin: "Devin",
  codex: "Codex",
};

const harnesses = targets.map((target) => ({
  id: target.id,
  label: displayNames[target.id] ?? target.label,
}));
</script>

<template>
  <section class="compatibility" aria-labelledby="compatibility-title">
    <div class="am-wrap strip">
      <div class="message">
        <span class="signal" aria-hidden="true"></span>
        <span id="compatibility-title"
          >Native output for the tools you already use</span
        >
      </div>

      <ul aria-label="Supported harnesses">
        <li v-for="harness in harnesses" :key="harness.id">
          {{ harness.label }}
        </li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.compatibility {
  border-block: 1px solid var(--am-border);
  background: var(--am-surface);
}

.strip {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 48px;
  align-items: center;
  min-height: 108px;
}

.message {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--vp-c-text-2);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.03em;
  white-space: nowrap;
}

.signal {
  width: 7px;
  height: 7px;
  flex: none;
  border-radius: 50%;
  background: var(--vp-c-brand-1);
  box-shadow: 0 0 18px
    color-mix(in srgb, var(--am-color-action-primary) 42%, transparent);
}

ul {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 10px 24px;
  margin: 0;
  padding: 0;
  list-style: none;
}

li {
  color: var(--vp-c-text-2);
  font-size: 14px;
  font-weight: 620;
  letter-spacing: -0.01em;
  white-space: nowrap;
}

@media (max-width: 820px) {
  .strip {
    grid-template-columns: 1fr;
    gap: 18px;
    padding-block: 24px;
  }

  ul {
    justify-content: flex-start;
  }
}

@media (max-width: 420px) {
  .message {
    white-space: normal;
  }
}
</style>
