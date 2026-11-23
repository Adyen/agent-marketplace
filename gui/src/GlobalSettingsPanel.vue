<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Configuration } from './api/client'
import type { WorkspaceController } from './controller'
import {
  draftFromSettings,
  settingsDraftErrors,
  settingsRequest,
  type InstallMode,
  type SettingsDraft,
  type SettingsDraftKey,
  type SettingsSection,
  type TelemetryChoice,
} from './settings-model'

type Settings = Configuration['settings']
type SettingField = Settings['backup']['enabled']

interface Option {
  value: string
  label: string
}

interface FieldUI {
  key: SettingsDraftKey
  label: string
  kind: 'switch' | 'number' | 'select' | 'text' | 'radio'
  field: (settings: Settings) => SettingField
  options?: Option[]
  placeholder?: string
  fallbackDefault?: string
  hint?: string
}

const props = defineProps<{
  controller: WorkspaceController
  section: SettingsSection
  settings: Settings
  userTargets: string[]
  disabled: boolean
}>()

const TELEMETRY_OPTIONS: Option[] = [
  { value: 'off', label: "Don't share" },
  { value: 'basic', label: 'Share basic telemetry' },
  { value: 'detailed', label: 'Share detailed telemetry' },
]
const INSTALL_OPTIONS: Option[] = [
  { value: 'symlink', label: 'Symlinks' },
  { value: 'file', label: 'Copies' },
]
const LOG_LEVEL_OPTIONS: Option[] = [
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warning' },
  { value: 'error', label: 'Error' },
]

const FIELDS: Record<SettingsSection, FieldUI[]> = {
  general: [
    { key: 'updateCheck', label: 'Show when a new release is available', kind: 'switch', field: (s) => s.updateCheck.enabled },
    { key: 'telemetryLevel', label: 'Telemetry', kind: 'radio', field: (s) => s.telemetry.level, options: TELEMETRY_OPTIONS, fallbackDefault: 'off' },
  ],
  install: [
    { key: 'installMode', label: 'Install resources as', kind: 'radio', field: (s) => s.materialization.mode, options: INSTALL_OPTIONS },
  ],
  backups: [
    { key: 'backupEnabled', label: 'Keep backups before changing agent files', kind: 'switch', field: (s) => s.backup.enabled },
    { key: 'backupMaxDays', label: 'Keep backups for N days', kind: 'number', field: (s) => s.backup.maxDays },
    { key: 'backupMaxPerDay', label: 'Keep at most N backups per day', kind: 'number', field: (s) => s.backup.maxPerDay },
  ],
  logging: [
    { key: 'logEnabled', label: 'Write a log file', kind: 'switch', field: (s) => s.logging.enabled },
    { key: 'logLevel', label: 'Log level', kind: 'select', field: (s) => s.logging.level, options: LOG_LEVEL_OPTIONS },
    { key: 'logFile', label: 'Log file path', kind: 'text', field: (s) => s.logging.file, placeholder: 'Default location' },
    { key: 'logMaxSizeMB', label: 'Rotate at N MB', kind: 'number', field: (s) => s.logging.maxSizeMB },
    { key: 'logMaxBackups', label: 'Keep N old logs', kind: 'number', field: (s) => s.logging.maxBackups },
    { key: 'logMaxAgeDays', label: 'Delete logs after N days', kind: 'number', field: (s) => s.logging.maxAgeDays },
    { key: 'logCompress', label: 'Compress old logs', kind: 'switch', field: (s) => s.logging.compress },
  ],
}

const TITLES: Record<SettingsSection, string> = {
  general: 'Updates and telemetry',
  install: 'Install mode',
  backups: 'Backups',
  logging: 'Logging',
}

const base = computed(() => draftFromSettings(props.settings))
const defaults = computed(() => draftFromSettings(props.settings, 'default'))
const draft = ref<SettingsDraft>(base.value)
const resets = ref<Set<SettingsDraftKey>>(new Set())
const saveError = ref<string | null>(null)
const errors = computed(() => settingsDraftErrors(props.section, draft.value))
const request = computed(() => settingsRequest(props.section, base.value, draft.value, resets.value))
const dirty = computed(() => request.value != null)
const hasErrors = computed(() => Object.keys(errors.value).length > 0)
const saveDisabled = computed(() => props.disabled || !dirty.value || hasErrors.value || props.controller.state.operation != null || props.controller.state.requesting)
const fields = computed(() => FIELDS[props.section])

watch(
  () => props.settings,
  () => discard(),
)

function discard() {
  draft.value = base.value
  resets.value = new Set()
  saveError.value = null
}

function isLocked(ui: FieldUI): boolean {
  return ui.field(props.settings).locked
}

function isDisabled(ui: FieldUI): boolean {
  return props.disabled || isLocked(ui)
}

function labelFor(value: string | null, options: Option[] | undefined): string {
  return options?.find((option) => option.value === value)?.label ?? (value == null ? 'Automatic' : value)
}

function formatValue(ui: FieldUI, value: unknown): string {
  if (typeof value === 'boolean') {
    return value ? 'On' : 'Off'
  }
  if (ui.options != null) {
    return labelFor(value as string | null, ui.options)
  }
  return value == null || value === '' ? 'Automatic' : String(value)
}

function helperText(ui: FieldUI): string | null {
  const source = ui.field(props.settings)
  if (source.locked) {
    return 'Set by your organization'
  }
  if (source.source === 'organization') {
    return `Organization default: ${formatValue(ui, source.value)}`
  }
  if (source.source === 'default') {
    return `Default: ${formatValue(ui, source.default ?? ui.fallbackDefault ?? null)}`
  }
  return null
}

function canReset(ui: FieldUI): boolean {
  return ui.field(props.settings).source === 'user' && !isDisabled(ui)
}

function reset(ui: FieldUI) {
  draft.value = { ...draft.value, [ui.key]: defaults.value[ui.key] }
  resets.value = new Set([...resets.value, ui.key])
}

function setDraft<K extends SettingsDraftKey>(key: K, value: SettingsDraft[K]) {
  draft.value = { ...draft.value, [key]: value }
  resets.value = new Set([...resets.value].filter((k) => k !== key))
}

function setTelemetry(value: string) {
  setDraft('telemetryLevel', value as TelemetryChoice)
}

function setInstallMode(value: string) {
  setDraft('installMode', value as InstallMode)
}

function setLevel(key: SettingsDraftKey, value: string) {
  setDraft(key, value as never)
}

function setTargetMode(id: string, value: string) {
  const next = { ...draft.value.targetModes }
  if (value === 'default') {
    delete next[id]
  } else {
    next[id] = value as InstallMode
  }
  setDraft('targetModes', next)
}

async function save() {
  const req = request.value
  if (req == null || hasErrors.value) {
    return
  }
  saveError.value = null
  if (!(await props.controller.saveSettings(req))) {
    saveError.value = props.controller.state.error ?? 'The action could not be started'
  }
}
</script>

<template>
  <section class="settings-window__section settings-global" :aria-label="TITLES[section]">
    <h3 class="gui-caps settings-global__title">{{ TITLES[section] }}</h3>

    <p v-if="saveError" role="alert" class="settings__notice settings__notice--danger">{{ saveError }}</p>

    <div v-for="ui in fields" :key="ui.key" class="settings-window__row settings-global__row">
      <div class="settings-window__row-text">
        <p class="settings-window__row-title">
          <label v-if="ui.kind === 'switch'" :for="`settings-${ui.key}`">{{ ui.label }}</label>
          <span v-else>{{ ui.label }}</span>
        </p>
        <p v-if="helperText(ui)" class="settings-window__row-hint">{{ helperText(ui) }}</p>
        <p v-if="errors[ui.key]" class="settings-global__error" role="alert">{{ errors[ui.key] }}</p>
        <button
          v-if="canReset(ui)"
          type="button"
          class="settings__link"
          :aria-label="`Reset ${ui.label}`"
          @click="reset(ui)"
        >Reset</button>
      </div>

      <input
        v-if="ui.kind === 'switch'"
        :id="`settings-${ui.key}`"
        type="checkbox"
        :aria-label="ui.label"
        :checked="draft[ui.key] as boolean"
        :disabled="isDisabled(ui)"
        @change="setDraft(ui.key, ($event.target as HTMLInputElement).checked as never)"
      />
      <input
        v-else-if="ui.kind === 'number'"
        type="number"
        min="0"
        step="1"
        class="gui-input settings-global__number"
        :aria-label="ui.label"
        :aria-invalid="errors[ui.key] != null"
        :value="draft[ui.key] as string"
        :disabled="isDisabled(ui)"
        @input="setDraft(ui.key, ($event.target as HTMLInputElement).value as never)"
      />
      <input
        v-else-if="ui.kind === 'text'"
        type="text"
        class="gui-input"
        :aria-label="ui.label"
        :placeholder="ui.placeholder"
        :value="draft[ui.key] as string"
        :disabled="isDisabled(ui)"
        @input="setDraft(ui.key, ($event.target as HTMLInputElement).value as never)"
      />
      <select
        v-else-if="ui.kind === 'select'"
        class="gui-input"
        :aria-label="ui.label"
        :value="draft[ui.key] as string"
        :disabled="isDisabled(ui)"
        @change="setLevel(ui.key, ($event.target as HTMLSelectElement).value)"
      >
        <option v-for="option in ui.options" :key="option.value" :value="option.value">{{ option.label }}</option>
      </select>
      <div v-else class="settings-global__choices" role="radiogroup" :aria-label="ui.label">
        <label v-for="option in ui.options" :key="option.value" class="settings-global__choice">
          <input
            type="radio"
            :name="`settings-${ui.key}`"
            :value="option.value"
            :aria-label="option.label"
            :checked="draft[ui.key] === option.value"
            :disabled="isDisabled(ui)"
            @change="ui.key === 'telemetryLevel' ? setTelemetry(option.value) : setInstallMode(option.value)"
          />
          <span>{{ option.label }}</span>
        </label>
      </div>
    </div>

    <div v-if="section === 'install' && userTargets.length > 0" class="settings-global__overrides">
      <p class="settings-window__row-title">Per-agent overrides</p>
      <p class="settings-window__row-hint">Choose a different install mode for one agent. Otherwise the default applies.</p>
      <div v-for="id in userTargets" :key="id" class="settings-global__override">
        <span class="mono">{{ id }}</span>
        <select
          class="gui-input"
          :aria-label="`Install mode for ${id}`"
          :value="draft.targetModes[id] ?? 'default'"
          :disabled="props.disabled"
          @change="setTargetMode(id, ($event.target as HTMLSelectElement).value)"
        >
          <option value="default">Use default</option>
          <option v-for="option in INSTALL_OPTIONS" :key="option.value" :value="option.value">{{ option.label }}</option>
        </select>
      </div>
    </div>

    <div class="settings-window__savebar">
      <span class="settings__status" aria-live="polite">
        <span v-if="dirty" class="settings__dirty"><span class="settings__dirty-dot" aria-hidden="true" />Unsaved changes</span>
      </span>
      <button type="button" class="gui-button gui-button--secondary" aria-label="Discard" :disabled="!dirty || props.disabled" @click="discard">Discard</button>
      <button type="button" class="gui-button" aria-label="Save changes" :disabled="saveDisabled" @click="save">Save changes</button>
    </div>
  </section>
</template>
