<script setup lang="ts">
withDefaults(
  defineProps<{
    href: string
    variant?: 'primary' | 'outline'
  }>(),
  { variant: 'primary' },
)
</script>

<template>
  <a
    class="action-link"
    :class="{ 'is-primary': variant === 'primary', 'is-outline': variant === 'outline' }"
    :href="href"
  >
    <slot />
  </a>
</template>

<style scoped>
.action-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-height: 46px;
  padding: 0 19px;
  border: 1px solid transparent;
  border-radius: var(--am-radius-lg);
  font-size: var(--am-text-body-sm);
  font-weight: var(--am-font-semibold);
  line-height: 1;
  text-decoration: none;
  transition:
    border-color 0.16s ease,
    background-color 0.16s ease,
    color 0.16s ease,
    transform 0.16s ease;
}

.action-link:hover {
  transform: translateY(-1px);
}

.action-link:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 3px;
}

.is-primary {
  border-color: var(--am-color-action-primary);
  background: var(--am-color-action-primary);
  color: var(--am-color-text-on-action);
}

.is-primary:hover {
  border-color: var(--am-color-action-hover);
  background: var(--am-color-action-hover);
  color: var(--am-color-text-on-action);
}

.is-outline {
  border-color: var(--vp-c-brand-1);
  background: var(--am-surface);
  color: var(--vp-c-brand-1);
}

.is-outline:hover {
  border-color: var(--vp-c-brand-2);
  background: var(--am-accent-soft);
  color: var(--vp-c-brand-2);
}

@media (prefers-reduced-motion: reduce) {
  .action-link {
    transition: none;
  }

  .action-link:hover {
    transform: none;
  }
}
</style>
