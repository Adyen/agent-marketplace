<script setup lang="ts">
type Props = {
  state: 'error' | 'empty'
}

defineProps<Props>()
const emit = defineEmits<{ retry: []; reset: [] }>()
</script>

<template>
  <UAlert v-if="state === 'error'" color="error" variant="subtle" icon="i-lucide-circle-alert" title="Search could not be completed" description="The Marketplace did not return results. Check the connection and try again." role="alert" class="mx-auto max-w-2xl">
    <template #actions>
      <UButton color="error" variant="solid" size="sm" @click="emit('retry')">Try again</UButton>
    </template>
  </UAlert>

  <div v-else role="status" aria-live="polite" class="rounded-2xl border border-dashed border-default bg-[var(--am-color-bg-surface)] px-6 py-16 text-center">
    <UIcon name="i-lucide-search-x" class="mx-auto size-10 text-dimmed" aria-hidden="true" />
    <h3 class="mt-4 text-lg font-semibold text-highlighted">No resources found</h3>
    <p class="mx-auto mt-2 max-w-md text-sm leading-6 text-toned">Try a broader search term or remove one of the filters to explore this Marketplace.</p>
    <UButton class="mt-6" color="neutral" variant="outline" @click="emit('reset')">Reset search</UButton>
  </div>
</template>
