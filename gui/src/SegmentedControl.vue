<script setup lang="ts">
defineProps<{
  name: string
  options: { value: string; label: string }[]
  modelValue: string
  disabled?: boolean
  fill?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <div class="segmented" :class="{ 'segmented--fill': fill }" role="radiogroup">
    <label
      v-for="option in options"
      :key="option.value"
      class="segmented__option"
      :class="{ 'segmented__option--selected': modelValue === option.value }"
    >
      <input
        type="radio"
        :name="name"
        :value="option.value"
        :checked="modelValue === option.value"
        :disabled="disabled"
        @change="emit('update:modelValue', option.value)"
      />
      <span>{{ option.label }}</span>
    </label>
  </div>
</template>
