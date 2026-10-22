<script setup lang="ts">
import { computed } from 'vue'
import GuiIcon, { type IconName } from './GuiIcon.vue'

const props = withDefaults(
  defineProps<{
    title: string
    icon: IconName
    size?: 'sm' | 'lg' | 'xl' | 'settings'
    dismissible?: boolean
    bare?: boolean
  }>(),
  { size: 'sm', dismissible: true, bare: false },
)
const open = defineModel<boolean>('open', { default: false })

const contentClass = computed(() => `gui-modal gui-modal--${props.size}`)
</script>

<template>
  <UModal
    v-model:open="open"
    :title="title"
    :aria-label="title"
    :dismissible="dismissible"
    :ui="{ content: contentClass, overlay: 'gui-scrim-modal' }"
  >
    <template #content>
      <section v-if="bare" class="gui-modal__frame gui-modal__frame--bare" :aria-label="title">
        <slot />
      </section>
      <section v-else class="gui-modal__frame" :aria-label="title">
        <header class="gui-modal__header">
          <span class="gui-modal__tile"><GuiIcon :name="icon" /></span>
          <button
            type="button"
            class="gui-modal__close"
            aria-label="Close dialog"
            :disabled="!dismissible"
            @click="open = false"
          ><GuiIcon name="close" /></button>
        </header>
        <h2 class="gui-modal__title">{{ title }}</h2>
        <div class="gui-modal__body">
          <slot />
        </div>
        <footer v-if="$slots.footer" class="gui-modal__footer">
          <slot name="footer" />
        </footer>
      </section>
    </template>
  </UModal>
</template>
