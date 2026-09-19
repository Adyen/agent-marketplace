<script setup lang="ts">
// Label + control + help/error wrapper with explicit ids, so native controls
// and Nuxt UI inputs share one layout and stay label-associated.
defineProps<{ id: string, label: string, description?: string, error?: string, required?: boolean }>()
</script>

<template>
  <div class="space-y-1.5">
    <label :for="id" class="block text-sm font-medium text-highlighted" :class="required && 'after:ml-0.5 after:text-muted after:content-[\'*\']'">{{ label }}</label>
    <slot />
    <p v-if="error" :id="`${id}-help`" class="text-sm text-error">{{ error }}</p>
    <p v-else-if="description" :id="`${id}-help`" class="text-xs leading-5 text-muted">{{ description }}</p>
  </div>
</template>
