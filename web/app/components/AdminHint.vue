<script setup lang="ts">
// Keeps honest caveats available without letting them dominate the layout.
// `text` renders as an icon tooltip; the optional slot renders as an
// expandable "What this means" note for longer explanations.
defineProps<{ text?: string, summary?: string }>()
</script>

<template>
  <UTooltip v-if="text" :text="text" :delay-duration="0" :content="{ side: 'bottom', sideOffset: 6 }" :ui="{ content: 'h-auto max-w-xs whitespace-normal rounded bg-inverted px-3 py-2 text-xs leading-5 text-inverted shadow-lg ring-0' }">
    <button type="button" :aria-label="text" class="inline-flex size-6 shrink-0 cursor-help items-center justify-center rounded-full text-muted hover:text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
      <UIcon name="i-lucide-info" class="size-4" aria-hidden="true" />
    </button>
  </UTooltip>
  <details v-else class="group rounded-lg border border-dashed border-default text-sm">
    <summary class="flex cursor-pointer items-center gap-2 px-3 py-2 font-medium text-muted hover:text-highlighted">
      <UIcon name="i-lucide-chevron-right" class="size-4 transition-transform group-open:rotate-90" aria-hidden="true" />{{ summary || 'What this means' }}
    </summary>
    <div class="space-y-2 px-3 pb-3 leading-6 text-toned"><slot /></div>
  </details>
</template>
