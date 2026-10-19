<script setup lang="ts">
import { targets as harnesses } from '../data/harnesses'
</script>

<template>
  <section class="compatibility" aria-labelledby="compatibility-title">
    <div class="am-wrap strip">
      <div class="message">
        <span class="signal" aria-hidden="true"></span>
        <span id="compatibility-title">Native output for the tools you already use</span>
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
  --strip-sans: "Adyen", "Inter", system-ui, sans-serif;
  --strip-mono: "Adyen Mono", "Geist Mono", ui-monospace, SFMono-Regular, monospace;

  border-block: 1px solid var(--am-border);
  background: var(--am-surface);
  font-feature-settings: "ss01", "ss02";
}

.strip {
  display: flex;
  max-width: 1440px;
  gap: 24px;
  align-items: center;
  min-height: 72px;
  padding-inline: 48px;
}

.message {
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--vp-c-text-1);
  font-family: var(--strip-mono);
  font-size: 12px;
  font-weight: 500;
  line-height: 1.3;
  letter-spacing: normal;
  text-transform: uppercase;
  white-space: nowrap;
}

.signal {
  width: 8px;
  height: 8px;
  flex: none;
  background: #00d16a;
}

ul {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px 12px;
  margin: 0;
  margin-left: auto;
  padding: 0;
  list-style: none;
}

li {
  color: var(--vp-c-text-3);
  font-family: var(--strip-mono);
  font-size: 12px;
  font-weight: 500;
  line-height: 1.3;
  letter-spacing: normal;
  text-transform: uppercase;
  white-space: nowrap;
}

li + li::before {
  margin-right: 12px;
  color: var(--vp-c-text-3);
  content: "·";
}

@media (max-width: 900px) {
  .strip {
    align-items: flex-start;
    flex-direction: column;
    gap: 12px;
    min-height: 0;
    padding-inline: 24px;
    padding-block: 24px;
  }

  ul {
    margin-left: 0;
    justify-content: flex-start;
  }
}

@media (max-width: 600px) {
  .strip {
    padding-inline: 16px;
  }

  .message {
    white-space: normal;
  }
}
</style>
