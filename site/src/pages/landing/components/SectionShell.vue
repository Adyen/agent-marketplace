<script setup lang="ts">
withDefaults(
  defineProps<{
    id?: string
    eyebrow: string
    title: string
    soft?: boolean
    density?: 'regular' | 'compact'
  }>(),
  { soft: false, density: 'regular' }
)
</script>

<template>
  <section
    :id="id"
    class="sec"
    :class="{ 'is-soft': soft, 'is-compact': density === 'compact' }"
  >
    <div class="am-wrap">
      <div class="head">
        <div class="head-copy">
          <span class="eyebrow"><span class="am-brand-slash" aria-hidden="true">/ </span>{{ eyebrow }}</span>
          <h2><slot name="title">{{ title }}</slot></h2>
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
  padding: var(--am-section-padding) 0;
  border-top: 1px solid var(--am-border);
  background: var(--vp-c-bg);
}

.is-soft {
  background:
    radial-gradient(circle at 82% 18%, color-mix(in srgb, var(--am-color-action-primary) 4.5%, transparent), transparent 28rem),
    var(--am-surface-soft);
}

.is-compact {
  --am-section-heading-gap: 28px;
  --am-section-padding: 72px;
}

.head {
  display: flex;
  max-width: var(--am-section-heading-width);
  align-items: flex-end;
  justify-content: space-between;
  gap: 32px;
  margin-bottom: var(--am-section-heading-gap);
}

.head-copy {
  min-width: 0;
}

.eyebrow {
  display: block;
  font-family: var(--vp-font-family-mono), monospace;
  font-size: var(--am-text-supporting);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--vp-c-text-1);
  margin-bottom: 14px;
}

h2 {
  margin: 0 0 10px;
  font-size: var(--am-section-heading-size);
  line-height: 1.12;
  letter-spacing: -0.035em;
  font-weight: var(--am-font-bold);
}

.sub {
  margin: 0;
  color: var(--vp-c-text-2);
  font-size: var(--am-text-body);
  line-height: 1.6;
}

@media (max-width: 680px) {
  .is-compact {
    --am-section-padding: 64px;
  }

  .head {
    align-items: flex-start;
    flex-direction: column;
    gap: 20px;
  }
}

</style>
