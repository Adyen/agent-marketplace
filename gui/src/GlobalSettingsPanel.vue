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
  unit?: string
  wide?: boolean
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
    { key: 'telemetryLevel', label: 'Telemetry', kind: 'radio', field: (s) => s.telemetry.level, options: TELEMETRY_OPTIONS },
  ],
  install: [
    { key: 'installMode', label: 'Install resources as', kind: 'radio', field: (s) => s.materialization.mode, options: INSTALL_OPTIONS },
  ],
  backups: [
    { key: 'backupEnabled', label: 'Keep backups before changing agent files', kind: 'switch', field: (s) => s.backup.enabled },
    { key: 'backupMaxDays', label: 'Keep backups for', kind: 'number', field: (s) => s.backup.maxDays, unit: 'days' },
    { key: 'backupMaxPerDay', label: 'Backups per day', kind: 'number', field: (s) => s.backup.maxPerDay },
  ],
  logging: [
    { key: 'logEnabled', label: 'Write a log file', kind: 'switch', field: (s) => s.logging.enabled },
    { key: 'logLevel', label: 'Log level', kind: 'select', field: (s) => s.logging.level, options: LOG_LEVEL_OPTIONS, wide: true },
    { key: 'logFile', label: 'Log file path', kind: 'text', field: (s) => s.logging.file, placeholder: 'Default location', wide: true },
    { key: 'logMaxSizeMB', label: 'Rotate log at', kind: 'number', field: (s) => s.logging.maxSizeMB, unit: 'MB' },
    { key: 'logMaxBackups', label: 'Keep old logs', kind: 'number', field: (s) => s.logging.maxBackups, unit: 'logs' },
    { key: 'logMaxAgeDays', label: 'Delete logs after', kind: 'number', field: (s) => s.logging.maxAgeDays, unit: 'days' },
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
  const differs = JSON.stringify(draft.value[ui.key]) !== JSON.stringify(defaults.value[ui.key])
  return differs ? `Default: ${formatValue(ui, defaults.value[ui.key])}` : null
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
  <section class="settings-global" :aria-label="TITLES[section]">
    <h3 v-if="section === 'general'" class="gui-caps settings-global__heading">Updates and telemetry</h3>

    <p v-if="saveError" role="alert" class="settings__notice settings__notice--danger">{{ saveError }}</p>

    <div class="settings-global__fields">
      <div v-for="ui in fields" :key="ui.key" class="settings-global__field">
        <div class="settings-global__label">
          <span class="settings-global__name">{{ ui.label }}</span>
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

        <div class="settings-global__control">
          <UCheckbox
            v-if="ui.kind === 'switch'"
            :model-value="draft[ui.key] as boolean"
            :aria-label="ui.label"
            :disabled="isDisabled(ui)"
            @update:model-value="setDraft(ui.key, ($event === true) as never)"
          />
          <div v-else-if="ui.kind === 'number'" class="settings-global__number-row">
            <input
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
            <span v-if="ui.unit" class="settings-global__unit">{{ ui.unit }}</span>
          </div>
          <input
            v-else-if="ui.kind === 'text'"
            type="text"
            class="gui-input settings-global__wide"
            :aria-label="ui.label"
            :placeholder="ui.placeholder"
            :value="draft[ui.key] as string"
            :disabled="isDisabled(ui)"
            @input="setDraft(ui.key, ($event.target as HTMLInputElement).value as never)"
          />
          <select
            v-else-if="ui.kind === 'select'"
            class="gui-input settings-global__wide"
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
      </div>
    </div>

    <div v-if="section === 'install' && userTargets.length > 0" class="settings-global__fields">
      <div v-for="id in userTargets" :key="id" class="settings-global__field">
        <div class="settings-global__label">
          <span class="settings-global__name mono">{{ id }}</span>
        </div>
        <div class="settings-global__control">
          <select
            class="gui-input settings-global__wide"
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
    </div>

    <div class="settings-window__savebar">
      <span class="settings__status" aria-live="polite">
        <span v-if="dirty" class="settings__dirty"><span class="settings__dirty-dot" aria-hidden="true" />Unsaved changes</span>
      </span>
      <UButton
        label="Discard"
        aria-label="Discard"
        color="neutral"
        variant="ghost"
        :disabled="!dirty || props.disabled"
        @click="discard"
      />
      <UButton
        label="Save changes"
        aria-label="Save changes"
        :disabled="saveDisabled"
        @click="save"
      />
    </div>
  </section>
</template>
