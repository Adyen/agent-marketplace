<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { observeDecode } from '../composables/eyebrowDecode'

const props = defineProps<{ text: string }>()

const root = ref<HTMLElement | null>(null)
const noise = ref('')
const decoding = ref(false)
let stop: (() => void) | undefined

onMounted(() => {
  if (!root.value) return
  stop = observeDecode(root.value, {
    text: props.text,
    onFrame: (value) => {
      noise.value = value
      decoding.value = true
    },
    onDone: () => (decoding.value = false),
  })
})

onBeforeUnmount(() => stop?.())
</script>

<template>
  <span ref="root" class="eyebrow-text">
    <span class="real" :class="{ 'is-decoding': decoding }">{{ text }}</span>
    <span v-if="decoding" class="noise" aria-hidden="true">{{ noise }}</span>
  </span>
</template>

<style scoped>
.eyebrow-text {
  position: relative;
  display: inline-block;
}

.real.is-decoding {
  opacity: 0;
}

.noise {
  position: absolute;
  top: 0;
  left: 0;
  pointer-events: none;
  white-space: pre;
}
</style>
