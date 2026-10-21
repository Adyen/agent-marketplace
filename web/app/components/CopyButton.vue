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
    class="copy-button inline-flex min-h-8 shrink-0 items-center rounded transition-colors disabled:shadow-none motion-reduce:transition-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--am-color-action-focus-ring)"
    :class="[
      disabled ? 'cursor-not-allowed text-(--am-color-text-muted)' : 'cursor-pointer text-(--am-color-text-primary)',
      iconOnly ? 'size-8 justify-center p-0' : 'gap-1.5 px-2',
      variant === 'ghost'
        ? ['border-0 bg-transparent', disabled ? '' : 'hover:bg-(--am-color-bg-hover)']
        : ['border border-(--am-color-border-default) bg-(--am-color-bg-subtle)', disabled ? '' : 'hover:border-(--am-color-border-strong) hover:bg-(--am-color-bg-hover)'],
    ]"
    :aria-label="copied ? copiedLabel : copyLabel"
    :title="copied ? copiedLabel : copyLabel"
    :disabled="disabled"
  >
    <UIcon :name="copied ? 'i-lucide-check' : 'i-lucide-copy'" class="size-3.5 shrink-0" aria-hidden="true" />
    <span v-if="!iconOnly">{{ copied ? copiedText : copyText }}</span>
  </button>
</template>

<style scoped>
.copy-button {
  font: var(--am-type-button);
}
</style>
