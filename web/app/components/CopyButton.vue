<script setup lang="ts">
type Props = {
  copied?: boolean
  copyLabel: string
  copiedLabel: string
  copyText?: string
  copiedText?: string
  disabled?: boolean
  iconOnly?: boolean
  variant?: 'default' | 'ghost'
}

withDefaults(defineProps<Props>(), {
  copied: false,
  copyText: 'Copy',
  copiedText: 'Copied',
  disabled: false,
  iconOnly: false,
  variant: 'default',
})
</script>

<template>
  <button
    type="button"
    class="inline-flex min-h-8 shrink-0 items-center rounded text-xs font-semibold text-[var(--am-color-text-primary)] transition-colors disabled:text-[var(--am-color-text-muted)] disabled:shadow-none motion-reduce:transition-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--am-color-action-focus-ring)]"
    :class="[
      disabled ? 'cursor-not-allowed' : 'cursor-pointer',
      iconOnly ? 'size-8 justify-center p-0' : 'gap-1.5 px-2',
      variant === 'ghost'
        ? 'border-0 bg-transparent hover:bg-[var(--am-color-bg-hover)] disabled:bg-transparent'
        : 'border border-[var(--am-color-border-default)] bg-[var(--am-color-bg-subtle)] hover:border-[var(--am-color-border-strong)] hover:bg-[var(--am-color-bg-hover)] disabled:border-[var(--am-color-border-default)] disabled:bg-[var(--am-color-bg-subtle)]',
    ]"
    :aria-label="copied ? copiedLabel : copyLabel"
    :title="copied ? copiedLabel : copyLabel"
    :disabled="disabled"
  >
    <UIcon :name="copied ? 'i-lucide-check' : 'i-lucide-copy'" class="size-3.5 shrink-0" aria-hidden="true" />
    <span v-if="!iconOnly">{{ copied ? copiedText : copyText }}</span>
  </button>
</template>
