<script setup lang="ts">
type Props = {
  copied?: boolean
  copyLabel: string
  copiedLabel: string
  copyText?: string
  copiedText?: string
  disabled?: boolean
}

withDefaults(defineProps<Props>(), {
  copied: false,
  copyText: 'Copy command',
  copiedText: 'Copied',
  disabled: false,
})
</script>

<template>
  <button
    type="button"
    class="inline-flex min-h-8 shrink-0 items-center gap-1.5 rounded border border-[var(--am-color-border-default)] bg-[var(--am-color-bg-subtle)] px-2 text-xs font-semibold text-[var(--am-color-text-primary)] transition-colors hover:border-[var(--am-color-border-strong)] hover:bg-[var(--am-color-bg-hover)] disabled:border-[var(--am-color-border-default)] disabled:bg-[var(--am-color-bg-subtle)] disabled:text-[var(--am-color-text-muted)] disabled:shadow-none motion-reduce:transition-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--am-color-action-focus-ring)]"
    :class="disabled ? 'cursor-not-allowed' : 'cursor-pointer'"
    :aria-label="copied ? copiedLabel : copyLabel"
    :title="copied ? copiedLabel : copyLabel"
    :disabled="disabled"
  >
    <UIcon :name="copied ? 'i-lucide-check' : 'i-lucide-copy'" class="size-3.5 shrink-0" aria-hidden="true" />
    {{ copied ? copiedText : copyText }}
  </button>
</template>
