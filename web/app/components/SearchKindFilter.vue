<script setup lang="ts">
import { computed } from 'vue'
import { ALL_RESOURCE_KINDS } from '~/types/marketplace'
import type { ResourceKind, ResourceKindFilter, SearchParams } from '~/types/marketplace'
import { kindLabel } from '~/lib/presentation'
import { fromResourceKindFilter, toResourceKindFilter } from '~/composables/useSearchQuery'

const props = defineProps<{
  modelValue: SearchParams['kind']
  kinds: ResourceKind[]
}>()
const emit = defineEmits<{
  'update:modelValue': [value: SearchParams['kind']]
}>()

const selectedKind = computed<ResourceKindFilter>({
  get: () => toResourceKindFilter(props.modelValue),
  set: value => emit('update:modelValue', fromResourceKindFilter(value)),
})
const items = computed(() => [
  { label: 'All resource kinds', value: ALL_RESOURCE_KINDS },
  ...props.kinds.map(kind => ({ label: kindLabel(kind), value: kind })),
])
</script>

<template>
  <USelect v-model="selectedKind" :items="items" value-key="value" />
</template>
