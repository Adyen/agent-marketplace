<script setup lang="ts">
import { computed, ref } from 'vue'
import { kindLabel } from '~/lib/presentation'
import { RESOURCE_KINDS, type RegistrySummary, type ResourceKind } from '~/types/marketplace'

const props = withDefaults(defineProps<{
  kind: ResourceKind | ''
  registry: string[]
  registries: RegistrySummary[]
  kinds?: ResourceKind[]
  idPrefix?: string
}>(), {
  kinds: () => [...RESOURCE_KINDS],
  idPrefix: 'catalog-filter',
})

const emit = defineEmits<{
  'update:kind': [value: ResourceKind | '']
  'update:registry': [value: string[]]
}>()

const INITIAL_REGISTRY_LIMIT = 7
const showAllRegistries = ref(false)

const registryOptions = computed(() => props.registries.map(registry => ({
  label: registry.slug || registry.id,
  value: registry.slug || registry.id,
})))

const visibleRegistries = computed(() => {
  if (showAllRegistries.value || registryOptions.value.length <= INITIAL_REGISTRY_LIMIT) {
    return registryOptions.value
  }

  const initial = registryOptions.value.slice(0, INITIAL_REGISTRY_LIMIT)
  const selected = registryOptions.value.filter(option => props.registry.includes(option.value))
  return [...new Map([...initial, ...selected].map(option => [option.value, option])).values()]
})

function updateKind(value: string) {
  emit('update:kind', value as ResourceKind | '')
}

function updateRegistry(value: string, checked: boolean) {
  const next = checked
    ? [...new Set([...props.registry, value])]
    : props.registry.filter(registry => registry !== value)

  emit('update:registry', next)
}
</script>

<template>
  <div class="catalog-filter-rail" :data-expanded="showAllRegistries">
    <fieldset class="catalog-filter-group">
      <legend class="catalog-filter-label">
        Kind
      </legend>
      <label class="catalog-filter-option">
        <input
          :name="`${idPrefix}-kind`"
          type="radio"
          value=""
          :checked="kind === ''"
          @change="updateKind('')"
        >
        <span>All kinds</span>
      </label>
      <label
        v-for="resourceKind in kinds"
        :key="resourceKind"
        class="catalog-filter-option"
      >
        <input
          :name="`${idPrefix}-kind`"
          type="radio"
          :value="resourceKind"
          :checked="kind === resourceKind"
          @change="updateKind(resourceKind)"
        >
        <span>{{ kindLabel(resourceKind) }}</span>
      </label>
    </fieldset>

    <fieldset class="catalog-filter-group">
      <legend class="catalog-filter-label">
        Registry
      </legend>
      <p
        v-if="registryOptions.length === 0"
        class="catalog-filter-empty"
      >
        No registries available
      </p>
      <label
        v-for="option in visibleRegistries"
        :key="option.value"
        class="catalog-filter-option"
      >
        <input
          type="checkbox"
          :value="option.value"
          :checked="registry.includes(option.value)"
          @change="updateRegistry(option.value, ($event.target as HTMLInputElement).checked)"
        >
        <span>{{ option.label }}</span>
      </label>
      <button
        v-if="registryOptions.length > INITIAL_REGISTRY_LIMIT"
        type="button"
        class="catalog-filter-more"
        :aria-expanded="showAllRegistries"
        @click="showAllRegistries = !showAllRegistries"
      >
        {{ showAllRegistries ? 'Show fewer' : `Show all (${registryOptions.length})` }}
      </button>
    </fieldset>
  </div>
</template>

<style scoped>
.catalog-filter-rail {
  display: grid;
  gap: 2rem;
}

.catalog-filter-group {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}

.catalog-filter-label {
  margin-bottom: 0.8rem;
  color: var(--am-color-text-secondary);
  font: var(--am-type-filter-heading);
}

.catalog-filter-option {
  display: grid;
  grid-template-columns: 1rem minmax(0, 1fr);
  align-items: center;
  gap: 0.65rem;
  min-height: 2rem;
  color: var(--am-color-text-secondary);
  font: var(--am-type-control);
  cursor: pointer;
}

.catalog-filter-option:hover {
  color: var(--am-color-text-primary);
}

.catalog-filter-option input {
  width: 1rem;
  height: 1rem;
  margin: 0;
  accent-color: var(--am-color-action-primary);
}

.catalog-filter-option input:focus-visible {
  outline: 2px solid var(--am-color-action-focus-ring);
  outline-offset: 3px;
}

.catalog-filter-option span {
  overflow-wrap: anywhere;
}

.catalog-filter-empty {
  color: var(--am-color-text-muted);
  font: var(--am-type-metadata);
}

.catalog-filter-more {
  min-height: 2rem;
  margin-top: 0.2rem;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--am-color-action-primary);
  font: var(--am-type-button);
  cursor: pointer;
}

.catalog-filter-more:hover {
  color: var(--am-color-action-hover);
}

.catalog-filter-more:focus-visible {
  border-radius: 0.2rem;
  outline: 2px solid var(--am-color-action-focus-ring);
  outline-offset: 3px;
}
</style>
