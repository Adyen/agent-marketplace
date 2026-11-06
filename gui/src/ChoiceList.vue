<script setup lang="ts">
import GuiIcon, { type IconName } from './GuiIcon.vue'

export interface ChoiceOption {
  value: string
  label: string
  ariaLabel?: string
  icon?: IconName
}

defineProps<{
  name: string
  legend: string
  options: ChoiceOption[]
  modelValue: string | null
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <fieldset class="choice-list" :disabled="disabled">
    <legend class="choice-list__legend">{{ legend }}</legend>
    <label
      v-for="option in options"
      :key="option.value"
      class="choice-list__row"
      :class="{ 'choice-list__row--selected': modelValue === option.value }"
    >
      <GuiIcon v-if="option.icon" :name="option.icon" class="choice-list__icon" />
      <span class="choice-list__label">{{ option.label }}</span>
      <input
        type="radio"
        class="choice-list__input"
        :name="name"
        :value="option.value"
        :checked="modelValue === option.value"
        :aria-label="option.ariaLabel"
        @change="emit('update:modelValue', option.value)"
      />
      <GuiIcon v-if="modelValue === option.value" name="check" class="choice-list__check" />
    </label>
  </fieldset>
</template>
