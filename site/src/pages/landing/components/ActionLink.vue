<script setup lang="ts">
withDefaults(
  defineProps<{
    href: string
    label: string
    variant?: 'primary' | 'outline' | 'text'
    icon?: 'arrow' | 'external'
  }>(),
  { variant: 'primary' },
)
</script>

<template>
  <a
    class="action-link"
    :class="{
      'is-primary': variant === 'primary',
      'is-outline': variant === 'outline',
      'is-text': variant === 'text',
    }"
    :href="href"
    data-roll-on-hover
  >
    <span class="am-roll-label action-link__label">
      <span class="am-roll-face action-link__face">
        {{ label }}
        <svg v-if="icon" class="am-stroke-icon action-link__icon" aria-hidden="true" viewBox="0 0 16 16">
          <path v-if="icon === 'arrow'" d="M2 8h11M9 4l4 4-4 4" />
          <path v-else d="M5 3.75h7.25V11M12 4 4 12" />
        </svg>
      </span>
      <span class="am-roll-face am-roll-face-copy action-link__face" aria-hidden="true">
        {{ label }}
        <svg v-if="icon" class="am-stroke-icon action-link__icon" aria-hidden="true" viewBox="0 0 16 16">
          <path v-if="icon === 'arrow'" d="M2 8h11M9 4l4 4-4 4" />
          <path v-else d="M5 3.75h7.25V11M12 4 4 12" />
        </svg>
      </span>
    </span>
  </a>
</template>

<style scoped>
.action-link {
  position: relative;
  isolation: isolate;
  display: inline-flex;
  width: fit-content;
  min-height: var(--lp-h-control);
  align-items: center;
  justify-content: center;
  padding: 0 var(--lp-inset);
  border-radius: var(--lp-radius-control);
  font: var(--lp-font-action);
  text-decoration: none;
  white-space: nowrap;
}

.action-link::before {
  position: absolute;
  z-index: -1;
  inset: 0;
  border-radius: inherit;
  background: var(--lp-surface);
  content: "";
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--lp-motion-feedback) var(--lp-ease);
}

.action-link:hover::before {
  opacity: 1;
}

.action-link:active::before {
  opacity: 0;
}

.is-primary {
  background: var(--lp-green);
  color: var(--lp-on-green);
}

.is-primary::before {
  background: var(--lp-hover-on-green);
}

.is-outline {
  background: var(--lp-fill);
  color: var(--lp-text);
}

.is-text {
  padding-inline: var(--lp-space-related);
  background: transparent;
  color: var(--lp-text);
}

/* The clip box carries the ink so descenders and icons are never cut. */
.action-link__label {
  margin: -0.125rem -0.25rem -0.1875rem;
  padding: 0.125rem 0.25rem 0.1875rem;
}

.action-link__face {
  gap: var(--lp-space-tight);
}

.action-link__icon {
  stroke-linecap: square;
  stroke-linejoin: miter;
}

@media (prefers-reduced-motion: reduce) {
  .action-link::before {
    transition: none;
  }
}
</style>
