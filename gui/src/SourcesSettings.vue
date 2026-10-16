<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Configuration, SourceDescription, SourceInput } from './api/client'
import type { WorkspaceController } from './controller'
import GuiModal from './GuiModal.vue'
import SourceForm from './SourceForm.vue'
import SourceList from './SourceList.vue'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })

const formEpoch = ref(0)
const pendingRemove = ref<SourceDescription | null>(null)
const loading = ref(false)
const actionError = ref<string | null>(null)
let openEpoch = 0

const configuration = computed<Configuration | null>(() => props.controller.state.configuration)
const busy = computed(() => props.controller.state.operation != null || props.controller.state.requesting)
const mutationErrors = computed(() =>
  props.controller.state.diagnostics.filter(
    (d) => d.severity === 'error' && d.category === 'mutation',
  ),
)
const editable = computed(() => configuration.value?.editable === true)
const fieldsDisabled = computed(() => loading.value || busy.value || !editable.value)

function resetDraft() {
  formEpoch.value += 1
  pendingRemove.value = null
}

watch(open, () => {
  const epoch = ++openEpoch
  if (!open.value) {
    resetDraft()
    return
  }
  resetDraft()
  actionError.value = null
  loading.value = true
  void props.controller.loadConfiguration().then(() => {
    if (epoch !== openEpoch) {
      return
    }
    loading.value = false
    if (configuration.value == null) {
      actionError.value = 'Sources could not be loaded'
    }
  })
})

function removable(source: SourceDescription) {
  return source.kind === 'git' || source.kind === 'directory' || source.kind === 'marketplace'
}

async function add(source: SourceInput) {
  pendingRemove.value = null
  if (await props.controller.addSource(source)) {
    actionError.value = null
    return
  }
  actionError.value = props.controller.state.error ?? 'The action could not be started'
}

async function confirmRemove() {
  const source = pendingRemove.value
  pendingRemove.value = null
  if (source == null) {
    return
  }
  if (!(await props.controller.removeSource(source.id))) {
    actionError.value = props.controller.state.error ?? 'The action could not be started'
  }
}

function cancel() {
  resetDraft()
  open.value = false
}
</script>

<template>
  <GuiModal v-model:open="open" title="Sources" icon="folder">
    <p v-if="actionError" role="alert" class="settings__error">{{ actionError }}</p>
    <p v-for="d in mutationErrors" :key="d.code" role="alert" class="settings__error">{{ d.message }}</p>
    <p v-if="controller.state.operation" role="status">{{ controller.state.operation.kind }} running</p>
    <p v-if="configuration == null && !actionError" role="status">Loading sources…</p>
    <template v-if="configuration != null">
      <p v-if="!configuration.editable" role="status">The user configuration is not editable.</p>

      <SourceList title="User sources" :sources="configuration.userSources" :organization="false">
        <template #actions="{ source }">
          <UButton
            v-if="removable(source)"
            label="Remove"
            color="neutral"
            variant="outline"
            :aria-label="`Remove source ${source.location}`"
            :disabled="fieldsDisabled"
            @click="pendingRemove = source"
          />
        </template>
      </SourceList>

      <section v-if="pendingRemove != null" class="settings__group" aria-label="Confirm removal">
        <p role="status">Remove source {{ pendingRemove.location }}?</p>
        <UButton
          label="Confirm remove"
          aria-label="Confirm remove"
          :disabled="fieldsDisabled"
          @click="confirmRemove"
        />
        <UButton
          label="Cancel remove"
          color="neutral"
          variant="outline"
          aria-label="Cancel remove"
          @click="pendingRemove = null"
        />
      </section>

      <SourceList
        title="Organization sources"
        :sources="configuration.organizationSources"
        :organization="true"
      />

      <section v-if="configuration.managedOutsideGUI.length > 0" class="settings__group settings__card" aria-label="Managed outside the GUI">
        <h3>Managed outside the GUI</h3>
        <ul>
          <li v-for="label in configuration.managedOutsideGUI" :key="label">{{ label }} — managed outside the GUI</li>
        </ul>
      </section>

      <section class="settings__group" aria-label="Add user source">
        <h3>Add a source</h3>
        <SourceForm :key="formEpoch" :disabled="fieldsDisabled" @add="add" />
      </section>
    </template>
    <template #footer>
      <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel" @click="cancel" />
    </template>
  </GuiModal>
</template>
