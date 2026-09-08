<script setup lang="ts">
withDefaults(
  defineProps<{
    id?: string
    eyebrow: string
    title: string
    soft?: boolean
  }>(),
  { soft: false }
)
</script>

<template>
  <section
    :id="id"
    class="sec"
    :class="{ 'is-soft': soft }"
  >
    <div class="am-wrap">
      <div class="head">
        <div>
          <span class="eyebrow">{{ eyebrow }}</span>
          <h2><slot name="title">{{ title }}</slot></h2>
          <p v-if="$slots.sub" class="sub"><slot name="sub" /></p>
        </div>
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

.head {
  max-width: var(--am-section-heading-width);
  margin-bottom: var(--am-section-heading-gap);
}

.eyebrow {
  display: block;
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--vp-c-brand-1);
  margin-bottom: 14px;
}

h2 {
  margin: 0 0 10px;
  font-size: var(--am-section-heading-size);
  line-height: 1.12;
  letter-spacing: -0.035em;
  font-weight: 700;
}

.sub {
  margin: 0;
  color: var(--vp-c-text-2);
  font-size: 17px;
  line-height: 1.6;
}

</style>
