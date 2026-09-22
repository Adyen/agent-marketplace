<script setup lang="ts">
import { computed } from "vue";
import { shortId } from "~/lib/admin-presentation";
import { useCopyText } from "~/composables/useCopyText";

const props = defineProps<{ label: string; value: string | undefined }>();
const { copied, copyError, copy } = useCopyText();
const display = computed(() => shortId(props.value));
</script>

<template>
  <span
    v-if="value"
    class="inline-flex max-w-full items-center gap-1 rounded border border-default bg-[var(--am-color-bg-subtle)] px-1.5 py-0.5 text-xs"
  >
    <span class="text-muted">{{ label }}</span>
    <code class="truncate font-mono text-highlighted" :title="value">{{
      display
    }}</code>
    <button
      type="button"
      class="inline-flex size-5 shrink-0 items-center justify-center rounded text-muted hover:text-highlighted focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-primary"
      :aria-label="copied ? `${label} ID copied` : `Copy ${label} ID ${value}`"
      @click="copy(value, 'id')"
    >
      <UIcon
        :name="copied ? 'i-lucide-check' : 'i-lucide-copy'"
        class="size-3.5"
        aria-hidden="true"
      />
    </button>
    <span v-if="copyError" role="alert" class="text-error">Copy failed</span>
  </span>
</template>
