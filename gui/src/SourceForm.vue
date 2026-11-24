<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import type { SourceInput } from './api/client'
import { validateSourceInput, type SourceField } from './source-input'
import GuiIcon from './GuiIcon.vue'
import SegmentedControl from './SegmentedControl.vue'

const props = defineProps<{ disabled: boolean; serverLocationError?: string; busy?: boolean; primary?: boolean }>()
const emit = defineEmits<{
  add: [source: SourceInput]
  editLocation: []
}>()

const nativeApp = (window as Window & {
  go?: { main?: { App?: { ChooseDirectory?: (defaultPath: string) => Promise<string> } } }
}).go?.main?.App
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
const kindCopy: Record<SourceInput['kind'], { label: string; hint: string; placeholder: string }> = {
  directory: {
    label: 'Folder',
    hint: 'A folder on this computer that holds resources. It is read in place.',
    placeholder: '~/code/my-registry',
  },
  git: {
    label: 'Repository URL',
    hint: 'A Git repository, read at the branch, tag or commit you choose.',
    placeholder: 'https://github.com/org/repo.git',
  },
  marketplace: {
    label: 'Marketplace URL',
    hint: 'A Marketplace server. Its resources appear here to browse and install.',
    placeholder: 'https://marketplace.example.com',
  },
}
const copy = computed(() => kindCopy[kind.value])
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

async function browse() {
  const chooseDirectory = nativeApp?.ChooseDirectory
  if (chooseDirectory == null) return
  const selected = await chooseDirectory.call(nativeApp, location.value).catch(() => '')
  if (selected === '') return
  location.value = selected
  edit('location')
}

watch(kind, () => {
  touched.value.clear()
  if (props.serverLocationError != null) emit('editLocation')
})

function add() {
  if (addDisabled.value) return
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
      <SegmentedControl
        name="source-kind"
        fill
        :options="kindOptions"
        :model-value="kind"
        :disabled="disabled"
        @update:model-value="kind = $event as SourceInput['kind']"
      />
      <p class="source-form__hint">{{ copy.hint }}</p>
    </fieldset>
    <div class="gui-field">
      <label class="source-form__label" :for="`${idPrefix}-location`">{{ copy.label }}</label>
      <div class="source-form__location">
        <input
          :id="`${idPrefix}-location`"
          v-model="location"
          class="gui-input mono"
          aria-label="Source location"
          type="text"
          :placeholder="copy.placeholder"
          :disabled="disabled"
          :aria-invalid="locationError != null ? 'true' : undefined"
          :aria-describedby="locationError != null ? `${idPrefix}-location-error` : undefined"
          @input="edit('location')"
          @keydown.enter.prevent="add"
          @blur="blur('location')"
        />
        <UButton
          v-if="kind === 'directory' && nativeApp?.ChooseDirectory != null"
          label="Browse…"
          aria-label="Browse for folder"
          type="button"
          class="gui-button--secondary source-form__browse"
          color="neutral"
          variant="soft"
          @click="browse"
        />
        <span v-if="locationError" :id="`${idPrefix}-location-error`" class="source-form__error" role="alert">{{ locationError }}</span>
      </div>
    </div>
    <div v-if="kind === 'git'" class="source-form__pair">
      <label class="gui-field">
        <span class="source-form__label">Branch, tag or commit</span>
        <input
          v-model="refName"
          class="gui-input mono"
          aria-label="Source ref"
          type="text"
          placeholder="main"
          :disabled="disabled"
          :aria-invalid="refError != null ? 'true' : undefined"
          :aria-describedby="refError != null ? `${idPrefix}-ref-error` : undefined"
          @input="edit('ref')"
          @keydown.enter.prevent="add"
          @blur="blur('ref')"
        />
        <span v-if="refError" :id="`${idPrefix}-ref-error`" class="source-form__error" role="alert">{{ refError }}</span>
      </label>
      <label class="gui-field">
        <span class="source-form__label">Folder in repository</span>
        <input
          v-model="subpath"
          class="gui-input mono"
          aria-label="Source subpath"
          type="text"
          placeholder="optional"
          :disabled="disabled"
          @keydown.enter.prevent="add"
        />
      </label>
    </div>
    <div class="source-form__actions">
      <slot name="actions" />
      <UButton
        v-if="props.primary"
        :label="props.busy ? 'Adding…' : 'Add source'"
        :aria-label="props.busy ? 'Adding source' : 'Add source'"
        class="source-form__submit"
        :disabled="addDisabled"
        @click="add"
      />
      <UButton
        v-else
        :label="props.busy ? 'Adding…' : 'Add source'"
        :aria-label="props.busy ? 'Adding source' : 'Add source'"
        class="gui-button--secondary source-form__submit"
        color="neutral"
        variant="soft"
        :disabled="addDisabled"
        @click="add"
      >
        <template #leading><GuiIcon name="plus" /></template>
      </UButton>
    </div>
  </fieldset>
</template>
