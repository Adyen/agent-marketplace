<script setup lang="ts">
import { computed } from 'vue'
import type { ResourceKind, SearchParams } from '~/types/marketplace'
import { kindLabel } from '~/lib/presentation'

const props = defineProps<{
  modelValue: SearchParams['kind']
  kinds: ResourceKind[]
}>()
const emit = defineEmits<{
  'update:modelValue': [value: SearchParams['kind']]
}>()

const selectedKind = computed<ResourceKind | undefined>({
  get: () => props.modelValue || undefined,
  set: value => emit('update:modelValue', value ?? ''),
})
const items = computed(() => props.kinds.map(kind => ({ label: kindLabel(kind), value: kind })))
</script>

<template>
  <USelect
    v-model="selectedKind"
    :items="items"
    value-key="value"
    placeholder="All resource kinds"
    :content="{ bodyLock: false }"
    :ui="{ base: 'h-9 w-full px-3 text-sm', value: 'min-w-0 flex-1 truncate text-left', placeholder: 'min-w-0 flex-1 truncate text-left text-dimmed', itemLabel: 'truncate' }"
  />
</template>
