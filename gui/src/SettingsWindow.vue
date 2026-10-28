<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { Configuration, SourceDescription, SourceInput } from './api/client'
import { useAppearance, type AppearancePreference } from './appearance'
import type { WorkspaceController } from './controller'
import { operationLabel } from './feedback'
import GuiIcon, { type IconName } from './GuiIcon.vue'
import GuiModal from './GuiModal.vue'
import SourceForm from './SourceForm.vue'
import SourceList from './SourceList.vue'

export type SettingsTab = 'general' | 'targets' | 'sources'

const props = defineProps<{ controller: WorkspaceController; tab: SettingsTab }>()
const open = defineModel<boolean>('open', { default: false })

const tabs: { id: SettingsTab; label: string; icon: IconName }[] = [
  { id: 'general', label: 'General', icon: 'gear' },
  { id: 'targets', label: 'Harness targets', icon: 'target' },
  { id: 'sources', label: 'Sources', icon: 'folder' },
]
const active = ref<SettingsTab>(props.tab)
const activeTitle = computed(() => tabs.find((tab) => tab.id === active.value)?.label ?? '')

const configuration = computed<Configuration | null>(() => props.controller.state.configuration)
const loading = ref(false)
const actionError = ref<string | null>(null)
const draft = ref<Set<string>>(new Set())
const formEpoch = ref(0)
const pendingRemove = ref<SourceDescription | null>(null)
let openEpoch = 0

const { preference, setPreference } = useAppearance()
const appearanceOptions: { value: AppearancePreference; label: string }[] = [
  { value: 'system', label: 'System' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
]

const options = computed<string[]>(() => configuration.value?.supportedTargets ?? [])

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
const mutationErrors = computed(() =>
  props.controller.state.diagnostics.filter((d) => d.severity === 'error' && d.category === 'mutation'),
)
const managedOutsideText = computed(() => {
  const list = new Intl.ListFormat('en', { style: 'long', type: 'conjunction' }).format(configuration.value?.managedOutsideGUI ?? [])
  return `${list.charAt(0).toUpperCase()}${list.slice(1)} are managed outside the GUI.`
})

function resetDrafts() {
  draft.value = new Set<string>()
  pendingRemove.value = null
  formEpoch.value += 1
}

watch(open, () => {
  const epoch = ++openEpoch
  resetDrafts()
  if (!open.value) {
    return
  }
  active.value = props.tab
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

watch(
  () => props.tab,
  (tab) => {
    active.value = tab
  },
)

function selectTab(id: SettingsTab) {
  active.value = id
}

function onTabKey(event: KeyboardEvent, index: number) {
  const last = tabs.length - 1
  let next: number | null = null
  if (event.key === 'ArrowDown') {
    next = index === last ? 0 : index + 1
  } else if (event.key === 'ArrowUp') {
    next = index === 0 ? last : index - 1
  } else if (event.key === 'Home') {
    next = 0
  } else if (event.key === 'End') {
    next = last
  }
  if (next == null) {
    return
  }
  event.preventDefault()
  selectTab(tabs[next].id)
  void nextTick(() => {
    const list = (event.currentTarget as HTMLElement).parentElement
    list?.querySelectorAll<HTMLElement>('[role="tab"]')[next]?.focus()
  })
}

function toggle(id: string, checked: boolean | 'indeterminate') {
  const next = new Set(draft.value)
  if (checked === true) {
    next.add(id)
  } else {
    next.delete(id)
  }
  draft.value = next
}

function revert() {
  draft.value = new Set(configuration.value?.userTargets ?? [])
}

async function save() {
  const targets = options.value.filter((id) => draft.value.has(id))
  if (await props.controller.saveTargets(targets)) {
    actionError.value = null
    draft.value = new Set(targets)
    return
  }
  actionError.value = props.controller.state.error ?? 'The action could not be started'
}

function removable(source: SourceDescription) {
  return source.kind === 'git' || source.kind === 'directory' || source.kind === 'marketplace'
}

async function add(source: SourceInput) {
  pendingRemove.value = null
  const accepted = await props.controller.addSource(source)
  if (accepted) {
    actionError.value = null
  } else {
    actionError.value = props.controller.state.error ?? 'The action could not be started'
  }
  void nextTick(() => {
    if (document.activeElement == null || document.activeElement === document.body) {
      document.querySelector<HTMLElement>('#settings-panel-sources')?.focus()
    }
  })
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
</script>

<template>
  <GuiModal v-model:open="open" title="Settings" icon="gear" size="settings" bare>
    <div class="settings-window">
      <nav class="settings-window__rail" aria-label="Settings">
        <p class="gui-caps settings-window__rail-label">Settings</p>
        <div role="tablist" aria-orientation="vertical" aria-label="Settings sections" class="settings-window__tabs">
          <button
            v-for="(tab, index) in tabs"
            :id="`settings-tab-${tab.id}`"
            :key="tab.id"
            type="button"
            role="tab"
            class="settings-window__tab"
            :class="{ 'settings-window__tab--selected': active === tab.id }"
            :aria-selected="active === tab.id"
            :aria-controls="`settings-panel-${tab.id}`"
            :tabindex="active === tab.id ? 0 : -1"
            @click="selectTab(tab.id)"
            @keydown="onTabKey($event, index)"
          >
            <GuiIcon :name="tab.icon" />
            <span class="settings-window__tab-label">{{ tab.label }}</span>
          </button>
        </div>
      </nav>

      <div class="settings-window__pane">
        <header class="settings-window__pane-header">
          <h2 class="settings-window__pane-title">{{ activeTitle }}</h2>
          <button type="button" class="gui-modal__close" aria-label="Close dialog" @click="open = false"><GuiIcon name="close" /></button>
        </header>

        <div class="settings-window__content">
          <p v-if="actionError" role="alert" class="settings__notice settings__notice--danger">
            <GuiIcon name="error" class="settings__notice-icon" />
            <span>{{ actionError }}</span>
          </p>
          <p v-for="d in mutationErrors" :key="d.code" role="alert" class="settings__notice settings__notice--danger">
            <GuiIcon name="error" class="settings__notice-icon" />
            <span>{{ d.message }}</span>
          </p>
          <p v-if="controller.state.operation" role="status" class="settings__notice">
            <GuiIcon name="info" class="settings__notice-icon" />
            <span>{{ operationLabel(controller.state.operation.kind) }}…</span>
          </p>
          <p v-if="configuration == null && !actionError" role="status" class="settings__notice settings__notice--loading">Loading settings…</p>
          <p v-if="configuration != null && !configuration.editable" role="status" class="settings__notice">
            <GuiIcon name="info" class="settings__notice-icon" />
            <span>The user configuration is not editable.</span>
          </p>

          <section
            v-show="active === 'general'"
            id="settings-panel-general"
            role="tabpanel"
            aria-labelledby="settings-tab-general"
            class="settings-window__panel"
          >
            <fieldset class="settings-window__section" aria-label="Appearance">
              <legend class="gui-caps">Appearance</legend>
              <div class="settings-window__row">
                <div class="settings-window__row-text">
                  <p id="settings-theme-label" class="settings-window__row-title">Theme</p>
                  <p class="settings-window__row-hint">Follow the system, or pick a theme for this app.</p>
                </div>
                <div class="theme-tiles" role="radiogroup" aria-labelledby="settings-theme-label">
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
              </div>
              <p class="settings__caption">Applies immediately.</p>
            </fieldset>
          </section>

          <section
            v-show="active === 'targets'"
            id="settings-panel-targets"
            role="tabpanel"
            aria-labelledby="settings-tab-targets"
            class="settings-window__panel settings-window__panel--targets"
          >
            <fieldset class="settings-window__section" :disabled="fieldsDisabled">
              <legend class="gui-caps">Harness targets</legend>
              <p class="settings__hint">Synced resources are written into each selected harness.</p>
              <div class="settings__list">
                <label v-for="id in options" :key="id" class="settings__option">
                  <span class="settings__row-main">
                    <span class="mono settings__target">{{ id }}</span>
                    <span v-if="organizationIds.has(id)" class="settings__row-hint">Also set by organization</span>
                  </span>
                  <UCheckbox
                    :model-value="draft.has(id)"
                    :aria-label="`Target ${id}`"
                    @update:model-value="toggle(id, $event)"
                  />
                </label>
              </div>
            </fieldset>

            <section v-if="configuration != null && configuration.organizationTargets.length > 0" class="settings-window__section" aria-label="Organization targets">
              <h3 class="gui-caps">Organization</h3>
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

            <div class="settings-window__savebar">
              <span class="settings__status" aria-live="polite">
                <span v-if="dirty" class="settings__dirty"><span class="settings__dirty-dot" aria-hidden="true" />Unsaved changes</span>
              </span>
              <UButton
                label="Revert"
                aria-label="Revert"
                color="neutral"
                variant="outline"
                :disabled="!dirty || fieldsDisabled"
                @click="revert"
              />
              <UButton
                v-if="configuration != null"
                label="Save targets"
                aria-label="Save targets"
                :disabled="saveDisabled"
                @click="save"
              />
            </div>
          </section>

          <section
            v-show="active === 'sources'"
            id="settings-panel-sources"
            role="tabpanel"
            tabindex="-1"
            aria-labelledby="settings-tab-sources"
            class="settings-window__panel"
          >
            <template v-if="configuration != null">
              <SourceList title="User sources" :sources="configuration.userSources" :organization="false">
                <template #actions="{ source }">
                  <button
                    v-if="removable(source)"
                    type="button"
                    class="gui-icon-button gui-icon-button--danger"
                    :aria-label="`Remove source ${source.location}`"
                    :disabled="fieldsDisabled"
                    @click="pendingRemove = source"
                  ><GuiIcon name="trash" /></button>
                </template>
              </SourceList>

              <section v-if="pendingRemove != null" class="settings__group" aria-label="Confirm removal">
                <p role="status">Remove source {{ pendingRemove.location }}?</p>
                <UButton label="Confirm remove" aria-label="Confirm remove" :disabled="fieldsDisabled" @click="confirmRemove" />
                <UButton label="Cancel remove" color="neutral" variant="outline" aria-label="Cancel remove" @click="pendingRemove = null" />
              </section>

              <SourceList title="Organization sources" :sources="configuration.organizationSources" :organization="true" />

              <section class="settings__group" aria-label="Add user source">
                <h3>Add a source</h3>
                <SourceForm :key="formEpoch" :disabled="fieldsDisabled" @add="add" />
              </section>

              <section v-if="configuration.managedOutsideGUI.length > 0" class="settings__footnote" aria-label="Managed outside the GUI">
                <GuiIcon name="info" class="settings__notice-icon" />
                <p>{{ managedOutsideText }}</p>
              </section>
            </template>
          </section>
        </div>
      </div>
    </div>
  </GuiModal>
</template>
