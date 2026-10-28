<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SourceInput } from './api/client'

const props = defineProps<{ disabled: boolean }>()
const emit = defineEmits<{ add: [source: SourceInput] }>()

const kind = ref<SourceInput['kind']>('directory')
const location = ref('')
const refName = ref('')
const subpath = ref('')

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

const addDisabled = computed(
  () =>
    props.disabled ||
    location.value === '' ||
    (kind.value === 'git' && refName.value === ''),
)

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
    <fieldset class="settings__group" aria-label="Source kind">
      <legend>Source kind</legend>
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
      Location
      <input
        v-model="location"
        class="gui-input"
        aria-label="Source location"
        type="text"
        :placeholder="locationPlaceholder"
        :disabled="disabled"
      />
    </label>
    <template v-if="kind === 'git'">
      <label class="gui-field">
        Ref
        <input v-model="refName" class="gui-input" aria-label="Source ref" type="text" placeholder="main" :disabled="disabled" />
      </label>
      <label class="gui-field">
        Subpath
        <input v-model="subpath" class="gui-input" aria-label="Source subpath" type="text" placeholder="optional" :disabled="disabled" />
      </label>
    </template>
    <UButton
      label="Add source"
      aria-label="Add source"
      class="gui-button--secondary"
      color="neutral"
      variant="soft"
      :disabled="addDisabled"
      @click="add"
    />
  </fieldset>
</template>
