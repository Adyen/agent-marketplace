<script setup lang="ts">
import { targets as harnesses } from '../data/harnesses'
</script>

<template>
  <section class="compatibility am-rule-block" aria-labelledby="compatibility-title">
    <div class="am-wrap strip">
      <div class="message">
        <span class="signal" aria-hidden="true"></span>
        <span id="compatibility-title">Native output for the tools you already use</span>
      </div>

      <ul aria-label="Supported harnesses">
        <li v-for="harness in harnesses" :key="harness.id" class="am-outline-chip">
          {{ harness.label }}
        </li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.compatibility {
  padding: var(--lp-space-default) 0;
}

.strip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--lp-space-related) var(--lp-space-default);
}

.message {
  display: flex;
  align-items: center;
  gap: 0.5625rem;
  color: var(--lp-text);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.signal {
  width: 0.5625rem;
  height: 0.5625rem;
  flex: none;
  background: var(--lp-green);
}

ul {
  display: flex;
  flex-wrap: wrap;
  gap: var(--lp-space-tight);
  margin: 0;
  padding: 0;
  list-style: none;
}

</style>
