<script setup lang="ts">
// Label + control + help/error wrapper with explicit ids, so native controls
// and Nuxt UI inputs share one layout and stay label-associated.
defineProps<{ id: string, label: string, description?: string, error?: string, required?: boolean }>()
</script>

<template>
  <div class="space-y-1.5" :data-invalid="error ? 'true' : undefined">
    <label :for="id" class="block text-sm font-medium" :class="[required && 'after:ml-0.5 after:text-muted after:content-[\'*\']', error ? 'text-error' : 'text-highlighted']">{{ label }}</label>
    <div :class="error && 'rounded-md ring-2 ring-error/70 ring-offset-1 ring-offset-[var(--am-color-bg-surface)]'"><slot /></div>
    <p v-if="error" :id="`${id}-help`" role="alert" class="flex items-start gap-1.5 text-sm text-error"><UIcon name="i-lucide-circle-alert" class="mt-0.5 size-4 shrink-0" aria-hidden="true" />{{ error }}</p>
    <p v-else-if="description" :id="`${id}-help`" class="text-xs leading-5 text-muted">{{ description }}</p>
  </div>
</template>
