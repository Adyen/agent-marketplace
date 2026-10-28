<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Row } from './controller'
import GuiIcon from './GuiIcon.vue'

const props = defineProps<{
  folded: boolean
  selected: Row | null
  actionDisabled: boolean
  overlay: boolean
}>()

const emit = defineEmits<{
  openResourceAction: [row: Row]
  openInspection: [row: Row]
}>()

type ButtonRef = { $el: HTMLElement } | HTMLElement | null
const openResourceButton = ref<{ $el: HTMLElement } | null>(null)
const resourceActionButton = ref<ButtonRef>(null)

const resourceActionVerbs: Partial<Record<Row['resourceAction'], string>> = {
  install: 'Install',
  remove: 'Remove',
  exclude: 'Exclude',
  restore: 'Restore',
}

const resourceActionVerb = computed(() => resourceActionVerbs[props.selected?.resourceAction ?? 'none'] ?? '')

const resourceActionLabel = computed(() => (resourceActionVerb.value ? `${resourceActionVerb.value} Resource` : 'Resource action'))

const resourceActionIcon = computed(() => props.selected?.resourceAction === 'remove' || props.selected?.resourceAction === 'exclude')

const statusTone = computed(() => (props.selected?.status === 'enabled' || props.selected?.status === 'installed' ? 'signal' : 'warning'))

defineExpose({
  focusOpenResource: () => openResourceButton.value?.$el.focus(),
  focusResourceAction(): boolean {
    const button = resourceActionButton.value
    const element = button instanceof HTMLElement ? button : button?.$el
    if (!(element instanceof HTMLButtonElement) || element.disabled) {
      return false
    }
    element.focus()
    return true
  },
})
</script>

<template>
  <aside
    aria-label="Detail"
    class="workspace__detail gui-inspector"
    :class="{ 'gui-inspector--overlay': overlay && !folded, 'gui-inspector--folded': folded }"
  >
    <header class="gui-inspector__header">
      <div class="gui-pane-title">Details</div>
    </header>
    <div class="gui-inspector__body">
      <section class="gui-inspector__section">
        <template v-if="selected">
          <div :key="selected.id" class="gui-inspector__resource">
            <div class="gui-inspector__head">
              <div class="gui-inspector__heading">
                <p class="gui-caps gui-inspector__eyebrow">{{ selected.kind }}</p>
                <h2 class="gui-inspector__title mono">{{ selected.displayName }}</h2>
              </div>
              <button
                v-if="resourceActionIcon"
                ref="resourceActionButton"
                type="button"
                class="gui-icon-button gui-icon-button--danger"
                :aria-label="resourceActionLabel"
                :title="resourceActionVerb"
                :disabled="actionDisabled"
                @click="emit('openResourceAction', selected)"
              ><GuiIcon name="trash" /></button>
            </div>
            <p class="gui-inspector__description">{{ selected.description }}</p>
            <dl>
              <dt>Registry</dt>
              <dd class="mono">{{ selected.registry }}</dd>
              <dt>Scope</dt>
              <dd>{{ selected.scope }}</dd>
              <dt>Status</dt>
              <dd><span class="gui-dot" :class="`gui-dot--${statusTone}`" aria-hidden="true" />{{ selected.status }}</dd>
              <dt>Harnesses</dt>
              <dd class="mono">{{ selected.harnesses.join(', ') || 'none' }}</dd>
            </dl>
            <div class="gui-inspector__actions">
              <UButton
                v-if="selected.resourceAction !== 'none' && !resourceActionIcon"
                ref="resourceActionButton"
                :aria-label="resourceActionLabel"
                class="gui-button--secondary"
                color="neutral"
                variant="soft"
                :disabled="actionDisabled"
                @click="emit('openResourceAction', selected)"
              >{{ resourceActionVerb }}</UButton>
              <UButton
                v-if="selected.inspectable || selected.installedFiles"
                ref="openResourceButton"
                aria-label="Open resource"
                class="gui-button--secondary"
                color="neutral"
                variant="soft"
                @click="emit('openInspection', selected)"
              >Open resource</UButton>
            </div>
          </div>
        </template>
        <div v-else class="gui-inspector__empty">
          <GuiIcon name="file" />
          <p>Select a resource to inspect it.</p>
        </div>
      </section>
    </div>
  </aside>
</template>
