<script setup lang="ts">
defineProps<{
  modelValue: string[]
  items: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
}>()
</script>

<template>
  <USelect
    :model-value="modelValue"
    :items="items"
    multiple
    placeholder="Any registry"
    :content="{ bodyLock: false }"
    :ui="{ base: 'h-9 w-full px-3 text-sm', value: 'min-w-0 flex-1 text-left', placeholder: 'min-w-0 flex-1 text-left text-dimmed', itemLabel: 'truncate' }"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <template #default>
      <span v-if="modelValue.length" class="flex min-w-0 flex-1 items-center gap-1.5 text-left">
        <span
          data-testid="registry-filter-first"
          class="min-w-0 flex-1 truncate"
          :title="modelValue[0]"
        >{{ modelValue[0] }}</span>
        <span
          v-if="modelValue.length > 1"
          data-testid="registry-filter-more"
          class="shrink-0 text-muted"
        >+{{ modelValue.length - 1 }} more</span>
      </span>
      <span v-else class="min-w-0 flex-1 truncate text-left text-dimmed">Any registry</span>
    </template>
  </USelect>
</template>
