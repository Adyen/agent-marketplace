<script setup lang="ts">
import EyebrowText from './EyebrowText.vue'

defineProps<{
  id?: string
  eyebrow: string
  title: string
}>()
</script>

<template>
  <section :id="id" class="sec">
    <div class="am-wrap">
      <div class="head">
        <div class="head-copy">
          <span class="eyebrow am-text-trim">
            <span class="am-brand-slash am-brand-slash--eyebrow" aria-hidden="true">/</span>
            <EyebrowText :text="eyebrow" />
          </span>
          <h2 class="am-text-trim"><slot name="title">{{ title }}</slot></h2>
          <p v-if="$slots.sub" class="sub"><slot name="sub" /></p>
        </div>
        <slot name="action" />
      </div>

      <slot />
    </div>
  </section>
</template>

<style scoped>
.sec {
  padding-top: var(--lp-edge-top, var(--lp-section-pad));
  padding-bottom: var(--lp-edge-bottom, var(--lp-section-pad));
  background: var(--lp-canvas);
}

.head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--lp-space-default);
  min-width: 0;
  margin-bottom: var(--lp-space-group);
}

.head-copy {
  min-width: 0;
  max-width: calc(50% - var(--lp-space-related));
}

.eyebrow {
  display: flex;
  align-items: center;
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

h2 {
  margin: var(--lp-space-default) 0 0;
  color: var(--lp-text);
  font: var(--lp-font-section);
  text-wrap: pretty;
}

.sub {
  max-width: var(--lp-measure);
  margin: var(--lp-space-default) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-body);
  text-wrap: pretty;
}

@media (max-width: 959px) {
  .head {
    flex-wrap: wrap;
  }

  .head-copy {
    max-width: none;
  }
}
</style>
