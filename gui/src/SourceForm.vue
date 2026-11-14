<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import type { SourceInput } from './api/client'
import { validateSourceInput, type SourceField } from './source-input'
import GuiIcon from './GuiIcon.vue'

const props = defineProps<{ disabled: boolean; serverLocationError?: string }>()
const emit = defineEmits<{
  add: [source: SourceInput]
  editLocation: []
}>()

const kind = ref<SourceInput['kind']>('directory')
const location = ref('')
const refName = ref('')
const subpath = ref('')
const touched = ref(new Set<SourceField>())
const idPrefix = useId()

const kindOptions: { value: SourceInput['kind']; label: string }[] = [
  { value: 'directory', label: 'Directory registry' },
  { value: 'git', label: 'Git registry' },
  { value: 'marketplace', label: 'Marketplace endpoint' },
]
const locationPlaceholders: Record<SourceInput['kind'], string> = {
  directory: '~/code/my-registry',
  git: 'https://github.com/org/repo.git',
  marketplace: 'https://marketplace.example.com',
}
const locationPlaceholder = computed(() => locationPlaceholders[kind.value])
const validationErrors = computed(() =>
  validateSourceInput({ kind: kind.value, location: location.value, ref: refName.value, path: subpath.value }),
)
const addDisabled = computed(() => props.disabled || Object.keys(validationErrors.value).length > 0)

function clientFieldError(field: SourceField): string | undefined {
  return touched.value.has(field) ? validationErrors.value[field] : undefined
}

const locationError = computed(() => props.serverLocationError ?? clientFieldError('location'))
const refError = computed(() => clientFieldError('ref'))

function edit(field: SourceField) {
  touched.value.delete(field)
  if (field === 'location') emit('editLocation')
}

function blur(field: SourceField) {
  touched.value.add(field)
}

watch(kind, () => {
  touched.value.clear()
  if (props.serverLocationError != null) emit('editLocation')
})

function add() {
  const source: SourceInput =
    kind.value === 'git'
      ? {
          kind: 'git',
          location: location.value,
          ref: refName.value,
          ...(subpath.value === '' ? {} : { path: subpath.value }),
        }
      : kind.value === 'marketplace'
        ? { kind: 'marketplace', location: location.value }
        : { kind: 'directory', location: location.value }
  emit('add', source)
}
</script>

<template>
  <fieldset class="source-form" :disabled="disabled">
    <fieldset class="source-form__kind settings__group" aria-label="Source kind">
      <legend class="source-form__label">Source kind</legend>
      <div class="segmented" role="radiogroup">
        <label
          v-for="option in kindOptions"
          :key="option.value"
          class="segmented__option"
          :class="{ 'segmented__option--selected': kind === option.value }"
        >
          <input v-model="kind" type="radio" name="source-kind" :value="option.value" :disabled="disabled" />
          <span>{{ option.label }}</span>
        </label>
      </div>
    </fieldset>
    <label class="gui-field">
      <span class="source-form__label">Location</span>
      <input
        v-model="location"
        class="gui-input"
        aria-label="Source location"
        type="text"
        :placeholder="locationPlaceholder"
        :disabled="disabled"
        :aria-invalid="locationError != null ? 'true' : undefined"
        :aria-describedby="locationError != null ? `${idPrefix}-location-error` : undefined"
        @input="edit('location')"
        @blur="blur('location')"
      />
      <span v-if="locationError" :id="`${idPrefix}-location-error`" class="source-form__error" role="alert">{{ locationError }}</span>
    </label>
    <template v-if="kind === 'git'">
      <label class="gui-field">
        <span class="source-form__label">Ref</span>
        <input
          v-model="refName"
          class="gui-input"
          aria-label="Source ref"
          type="text"
          placeholder="main"
          :disabled="disabled"
          :aria-invalid="refError != null ? 'true' : undefined"
          :aria-describedby="refError != null ? `${idPrefix}-ref-error` : undefined"
          @input="edit('ref')"
          @blur="blur('ref')"
        />
        <span v-if="refError" :id="`${idPrefix}-ref-error`" class="source-form__error" role="alert">{{ refError }}</span>
      </label>
      <label class="gui-field">
        <span class="source-form__label">Subpath</span>
        <input
          v-model="subpath"
          class="gui-input"
          aria-label="Source subpath"
          type="text"
          placeholder="optional"
          :disabled="disabled"
        />
      </label>
    </template>
    <UButton
      label="Add source"
      aria-label="Add source"
      class="gui-button--secondary source-form__submit"
      color="neutral"
      variant="soft"
      :disabled="addDisabled"
      @click="add"
    >
      <template #leading><GuiIcon name="plus" /></template>
    </UButton>
  </fieldset>
</template>
