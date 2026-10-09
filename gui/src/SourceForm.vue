<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SourceInput } from './api/client'

const props = defineProps<{ disabled: boolean }>()
const emit = defineEmits<{ add: [source: SourceInput] }>()

const kind = ref<SourceInput['kind']>('directory')
const location = ref('')
const refName = ref('')
const subpath = ref('')

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
  <fieldset :disabled="disabled">
    <fieldset class="settings__group" aria-label="Source kind">
      <legend>Source kind</legend>
      <label class="settings__option">
        <input v-model="kind" type="radio" name="source-kind" value="directory" :disabled="disabled" />
        Directory registry
      </label>
      <label class="settings__option">
        <input v-model="kind" type="radio" name="source-kind" value="git" :disabled="disabled" />
        Git registry
      </label>
      <label class="settings__option">
        <input v-model="kind" type="radio" name="source-kind" value="marketplace" :disabled="disabled" />
        Marketplace endpoint
      </label>
    </fieldset>
    <label class="settings__option">
      Location
      <input v-model="location" aria-label="Source location" type="text" :disabled="disabled" />
    </label>
    <template v-if="kind === 'git'">
      <label class="settings__option">
        Ref
        <input v-model="refName" aria-label="Source ref" type="text" :disabled="disabled" />
      </label>
      <label class="settings__option">
        Subpath
        <input v-model="subpath" aria-label="Source subpath" type="text" :disabled="disabled" />
      </label>
    </template>
    <UButton
      label="Add source"
      aria-label="Add source"
      :disabled="addDisabled"
      @click="add"
    />
  </fieldset>
</template>
