<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Configuration } from './api/client'
import { useAppearance, type AppearancePreference } from './appearance'
import GuiIcon from './GuiIcon.vue'
import GuiModal from './GuiModal.vue'
import type { WorkspaceController } from './controller'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ manageSources: [] }>()

const draft = ref<Set<string>>(new Set())
const loading = ref(false)
const actionError = ref<string | null>(null)
let openEpoch = 0

const { preference, setPreference } = useAppearance()
const appearanceOptions: { value: AppearancePreference; label: string }[] = [
  { value: 'system', label: 'System' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
]

const configuration = computed<Configuration | null>(() => props.controller.state.configuration)

interface TargetOption {
  id: string
  supported: boolean
}

const options = computed<TargetOption[]>(() => {
  const config = configuration.value
  if (config == null) {
    return []
  }
  const supported = new Set(config.supportedTargets)
  const listed: TargetOption[] = config.supportedTargets.map((id) => ({ id, supported: true }))
  for (const id of config.userTargets) {
    if (!supported.has(id)) {
      listed.push({ id, supported: false })
    }
  }
  return listed
})

const busy = computed(() => props.controller.state.operation != null || props.controller.state.requesting)
const editable = computed(() => configuration.value?.editable === true)
const fieldsDisabled = computed(() => loading.value || busy.value || !editable.value)
const saveDisabled = computed(() => loading.value || !editable.value || draft.value.size === 0 || busy.value)
const organizationIds = computed(() => new Set(configuration.value?.organizationTargets ?? []))
const dirty = computed(() => {
  if (loading.value || configuration.value == null) {
    return false
  }
  const saved = configuration.value.userTargets
  return draft.value.size !== saved.length || saved.some((id) => !draft.value.has(id))
})
const sourceSummary = computed(() => {
  const config = configuration.value
  if (config == null) {
    return ''
  }
  const total = config.userSources.length + config.organizationSources.length
  if (total === 0) {
    return 'No sources yet'
  }
  const organization = config.organizationSources.length
  return `${total} configured${organization > 0 ? ` · ${organization} from organization` : ''}`
})
const managedOutsideText = computed(() => {
  const list = new Intl.ListFormat('en', { style: 'long', type: 'conjunction' }).format(configuration.value?.managedOutsideGUI ?? [])
  return `${list.charAt(0).toUpperCase()}${list.slice(1)} are managed outside the GUI.`
})

watch(open, () => {
  const epoch = ++openEpoch
  if (!open.value) {
    return
  }
  draft.value = new Set<string>()
  actionError.value = null
  loading.value = true
  void props.controller.loadConfiguration().then(() => {
    if (epoch !== openEpoch) {
      return
    }
    loading.value = false
    if (configuration.value == null) {
      actionError.value = 'Settings could not be loaded'
      return
    }
    draft.value = new Set(configuration.value.userTargets)
  })
})

function toggle(id: string, checked: boolean | 'indeterminate') {
  const next = new Set(draft.value)
  if (checked === true) {
    next.add(id)
  } else {
    next.delete(id)
  }
  draft.value = next
}

function cancel() {
  draft.value = new Set<string>()
  open.value = false
}

function manageSources() {
  cancel()
  emit('manageSources')
}

async function save() {
  const order = options.value.map((option) => option.id)
  const targets = order.filter((id) => draft.value.has(id))
  for (const id of draft.value) {
    if (!order.includes(id)) {
      targets.push(id)
    }
  }
  if (await props.controller.saveTargets(targets)) {
    draft.value = new Set<string>()
    open.value = false
    return
  }
  actionError.value = props.controller.state.error ?? 'The action could not be started'
}
</script>

<template>
  <GuiModal v-model:open="open" title="Settings" icon="gear">
    <div class="settings-body">
      <p v-if="actionError" role="alert" class="settings__notice settings__notice--danger">
        <GuiIcon name="error" class="settings__notice-icon" />
        <span>{{ actionError }}</span>
      </p>
      <p v-if="configuration == null && !actionError" role="status" class="settings__notice settings__notice--loading">Loading settings…</p>
      <template v-if="configuration != null">
        <p v-if="!configuration.editable" role="status" class="settings__notice">
          <GuiIcon name="info" class="settings__notice-icon" />
          <span>The user configuration is not editable.</span>
        </p>
        <p v-else-if="busy" role="status" class="settings__notice">
          <GuiIcon name="info" class="settings__notice-icon" />
          <span>Settings are locked while an operation runs.</span>
        </p>

        <fieldset class="settings__group" :disabled="fieldsDisabled">
          <legend>Harness targets</legend>
          <p class="settings__hint">Synced resources are written into each selected harness.</p>
          <div class="settings__list">
            <label v-for="option in options" :key="option.id" class="settings__option">
              <span class="settings__row-main">
                <span class="mono settings__target">{{ option.id }}</span>
                <span v-if="!option.supported" class="settings__badge settings__badge--warning">Unsupported by this version</span>
                <span v-if="organizationIds.has(option.id)" class="settings__row-hint">Also set by organization</span>
              </span>
              <UCheckbox
                :model-value="draft.has(option.id)"
                :aria-label="`Target ${option.id}`"
                @update:model-value="toggle(option.id, $event)"
              />
            </label>
          </div>
        </fieldset>

        <section v-if="configuration.organizationTargets.length > 0" class="settings__group" aria-label="Organization targets">
          <h3>Organization</h3>
          <ul class="settings__list">
            <li v-for="id in configuration.organizationTargets" :key="id" class="settings__option settings__option--static">
              <span class="settings__row-main">
                <span class="mono settings__target">{{ id }}</span>
                <span class="settings__badge">ORG</span>
              </span>
              <GuiIcon name="lock" class="settings__lock" />
            </li>
          </ul>
          <p class="settings__caption">Managed by your organization. Always applied in addition to your targets.</p>
        </section>

        <fieldset class="settings__group" aria-label="Appearance">
          <legend>Appearance</legend>
          <div class="theme-tiles" role="radiogroup">
            <label
              v-for="option in appearanceOptions"
              :key="option.value"
              class="theme-tile"
              :class="{ 'theme-tile--selected': preference === option.value }"
            >
              <input
                class="gui-visually-hidden"
                type="radio"
                name="am-appearance"
                :value="option.value"
                :checked="preference === option.value"
                @change="setPreference(option.value)"
              />
              <span class="theme-tile__preview" aria-hidden="true">
                <span class="theme-tile__window" data-preview="light">
                  <span class="theme-tile__side" />
                  <span class="theme-tile__main">
                    <span class="theme-tile__bar" />
                    <span class="theme-tile__line" />
                    <span class="theme-tile__line theme-tile__line--short" />
                    <span class="theme-tile__line theme-tile__line--mid" />
                    <span class="theme-tile__accent" />
                  </span>
                </span>
                <span
                  v-if="option.value === 'dark' || option.value === 'system'"
                  class="theme-tile__window"
                  :class="{ 'theme-tile__window--diagonal': option.value === 'system' }"
                  data-preview="dark"
                >
                  <span class="theme-tile__side" />
                  <span class="theme-tile__main">
                    <span class="theme-tile__bar" />
                    <span class="theme-tile__line" />
                    <span class="theme-tile__line theme-tile__line--short" />
                    <span class="theme-tile__line theme-tile__line--mid" />
                    <span class="theme-tile__accent" />
                  </span>
                </span>
              </span>
              <span class="theme-tile__label">{{ option.label }}</span>
            </label>
          </div>
          <p class="settings__caption">Applies immediately.</p>
        </fieldset>

        <section class="settings__group" aria-label="Sources">
          <h3>Sources</h3>
          <button type="button" class="settings__nav" aria-label="Manage sources" @click="manageSources">
            <span class="settings__nav-tile"><GuiIcon name="folder" /></span>
            <span class="settings__nav-text">
              <span class="settings__nav-title">Manage sources</span>
              <span class="settings__nav-meta">{{ sourceSummary }}</span>
            </span>
            <GuiIcon name="chevronRight" class="settings__nav-chevron" />
          </button>
        </section>

        <section v-if="configuration.managedOutsideGUI.length > 0" class="settings__footnote" aria-label="Managed outside the GUI">
          <GuiIcon name="info" class="settings__notice-icon" />
          <p>{{ managedOutsideText }}</p>
        </section>
      </template>
    </div>
    <template #footer>
      <span class="settings__status" aria-live="polite"><span v-if="dirty" class="settings__dirty"><span class="settings__dirty-dot" aria-hidden="true" />Unsaved changes</span></span>
      <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel" @click="cancel" />
      <UButton
        v-if="configuration != null"
        label="Save targets"
        aria-label="Save targets"
        :disabled="saveDisabled"
        @click="save"
      />
    </template>
  </GuiModal>
</template>
