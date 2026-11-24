<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Configuration } from './api/client'
import ChoiceList from './ChoiceList.vue'
import type { WorkspaceController } from './controller'
import GuiSwitch from './GuiSwitch.vue'
import { harnessDisplayName } from './harness-names'
import SegmentedControl from './SegmentedControl.vue'
import {
  draftFromSettings,
  settingsDraftErrors,
  settingsRequest,
  type InstallMode,
  type SettingsDraft,
  type SettingsDraftKey,
  type SettingsSection,
} from './settings-model'

type Settings = Configuration['settings']
type SettingField = Settings['backup']['enabled']

interface Option {
  value: string
  label: string
  hint?: string
}

interface FieldUI {
  key: SettingsDraftKey
  label: string
  kind: 'switch' | 'number' | 'segmented' | 'text' | 'choice'
  field: (settings: Settings) => SettingField
  hint?: string
  options?: Option[]
  placeholder?: string
  unit?: string
  dependsOn?: SettingsDraftKey
}

const props = defineProps<{
  controller: WorkspaceController
  section: SettingsSection
  settings: Settings
  userTargets: string[]
  disabled: boolean
}>()

const TELEMETRY_OPTIONS: Option[] = [
  { value: 'off', label: "Don't share", hint: 'Nothing is sent.' },
  {
    value: 'basic',
    label: 'Share basic telemetry',
    hint: 'App version, OS, command names, agent types, outcome and rough duration. Never command arguments.',
  },
  {
    value: 'detailed',
    label: 'Share detailed telemetry',
    hint: "Everything in basic, plus your configuration, including registry and resource names, and a trace of each command's steps.",
  },
]
const INSTALL_OPTIONS: Option[] = [
  { value: 'symlink', label: 'Symlinks', hint: 'Agent folders link to one cached copy, so nothing is duplicated.' },
  { value: 'file', label: 'Copies', hint: "Regular files are written into each agent folder. Use this where links aren't followed." },
]
const TARGET_MODE_OPTIONS: Option[] = [{ value: 'default', label: 'Default' }, ...INSTALL_OPTIONS]
const LOG_LEVEL_OPTIONS: Option[] = [
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warning' },
  { value: 'error', label: 'Error' },
]

const FIELDS: Record<SettingsSection, FieldUI[]> = {
  general: [
    {
      key: 'updateCheck',
      label: 'Show when a new release is available',
      kind: 'switch',
      field: (s) => s.updateCheck.enabled,
      hint: 'A notice appears at the top of the window.',
    },
    { key: 'telemetryLevel', label: 'Telemetry', kind: 'choice', field: (s) => s.telemetry.level, options: TELEMETRY_OPTIONS },
  ],
  install: [
    {
      key: 'installMode',
      label: 'Install resources as',
      kind: 'choice',
      field: (s) => s.materialization.mode,
      hint: 'Applies to skills, commands and subagents.',
      options: INSTALL_OPTIONS,
    },
  ],
  backups: [
    {
      key: 'backupEnabled',
      label: 'Keep backups before changing agent files',
      kind: 'switch',
      field: (s) => s.backup.enabled,
      hint: 'Saved in ~/.agent-marketplace/backups.',
    },
    {
      key: 'backupMaxDays',
      label: 'Keep backups for',
      kind: 'number',
      field: (s) => s.backup.maxDays,
      unit: 'days',
      hint: 'Backups from earlier days are deleted.',
      dependsOn: 'backupEnabled',
    },
    {
      key: 'backupMaxPerDay',
      label: 'Backups per day',
      kind: 'number',
      field: (s) => s.backup.maxPerDay,
      hint: 'When a day has more, the oldest are deleted.',
      dependsOn: 'backupEnabled',
    },
  ],
  logging: [
    { key: 'logEnabled', label: 'Write a log file', kind: 'switch', field: (s) => s.logging.enabled },
    {
      key: 'logLevel',
      label: 'Log level',
      kind: 'segmented',
      field: (s) => s.logging.level,
      hint: 'Debug records the most, Error the least.',
      options: LOG_LEVEL_OPTIONS,
      dependsOn: 'logEnabled',
    },
    {
      key: 'logFile',
      label: 'Log file path',
      kind: 'text',
      field: (s) => s.logging.file,
      placeholder: '~/.agent-marketplace/logs/agent-marketplace.log',
      dependsOn: 'logEnabled',
    },
    {
      key: 'logMaxSizeMB',
      label: 'Rotate log at',
      kind: 'number',
      field: (s) => s.logging.maxSizeMB,
      unit: 'MB',
      hint: 'A new file starts at this size.',
      dependsOn: 'logEnabled',
    },
    { key: 'logMaxBackups', label: 'Keep old logs', kind: 'number', field: (s) => s.logging.maxBackups, unit: 'logs', dependsOn: 'logEnabled' },
    { key: 'logMaxAgeDays', label: 'Delete logs after', kind: 'number', field: (s) => s.logging.maxAgeDays, unit: 'days', dependsOn: 'logEnabled' },
    { key: 'logCompress', label: 'Compress old logs', kind: 'switch', field: (s) => s.logging.compress, dependsOn: 'logEnabled' },
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

function isOff(ui: FieldUI): boolean {
  return ui.dependsOn != null && draft.value[ui.dependsOn] === false
}

function isDisabled(ui: FieldUI): boolean {
  return props.disabled || isLocked(ui) || isOff(ui)
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
    <p v-if="saveError" role="alert" class="settings__notice settings__notice--danger">{{ saveError }}</p>

    <div class="settings-global__fields">
      <div
        v-for="ui in fields"
        :key="ui.key"
        class="settings-global__field"
        :class="{ 'settings-global__field--stacked': ui.kind === 'choice', 'settings-global__field--off': isOff(ui) }"
      >
        <div class="settings-global__label">
          <span class="settings-global__name">{{ ui.label }}</span>
          <p v-if="ui.hint" class="settings-window__row-hint">{{ ui.hint }}</p>
          <p v-if="helperText(ui) || canReset(ui)" class="settings-global__meta">
            <span v-if="helperText(ui)">{{ helperText(ui) }}</span>
            <button
              v-if="canReset(ui)"
              type="button"
              class="settings__link"
              :aria-label="`Reset ${ui.label}`"
              @click="reset(ui)"
            >Reset</button>
          </p>
          <p v-if="errors[ui.key]" class="settings-global__error" role="alert">{{ errors[ui.key] }}</p>
        </div>

        <div class="settings-global__control">
          <GuiSwitch
            v-if="ui.kind === 'switch'"
            :model-value="draft[ui.key] as boolean"
            :aria-label="ui.label"
            :disabled="isDisabled(ui)"
            @update:model-value="setDraft(ui.key, $event as never)"
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
            <span class="settings-global__unit">{{ ui.unit }}</span>
          </div>
          <input
            v-else-if="ui.kind === 'text'"
            type="text"
            class="gui-input settings-global__path"
            :aria-label="ui.label"
            :placeholder="ui.placeholder"
            :value="draft[ui.key] as string"
            :disabled="isDisabled(ui)"
            @input="setDraft(ui.key, ($event.target as HTMLInputElement).value as never)"
          />
          <SegmentedControl
            v-else-if="ui.kind === 'segmented'"
            :name="`settings-${ui.key}`"
            :aria-label="ui.label"
            :options="ui.options ?? []"
            :model-value="draft[ui.key] as string"
            :disabled="isDisabled(ui)"
            @update:model-value="setDraft(ui.key, $event as never)"
          />
          <ChoiceList
            v-else
            :name="`settings-${ui.key}`"
            :aria-label="ui.label"
            :options="(ui.options ?? []).map((option) => ({ ...option, ariaLabel: option.label }))"
            :model-value="draft[ui.key] as string"
            :disabled="isDisabled(ui)"
            @update:model-value="setDraft(ui.key, $event as never)"
          />
        </div>
      </div>
    </div>

    <section v-if="section === 'install' && userTargets.length > 0" class="settings-global__group" aria-label="Per-agent install mode">
      <h3 class="settings-global__name">For one agent</h3>
      <p class="settings-window__row-hint">Default follows the choice above.</p>
      <div class="settings-global__fields">
        <div v-for="id in userTargets" :key="id" class="settings-global__field">
          <span class="settings-global__agent">{{ harnessDisplayName(id) }}</span>
          <SegmentedControl
            :name="`settings-target-mode-${id}`"
            :aria-label="`Install mode for ${id}`"
            :options="TARGET_MODE_OPTIONS"
            :model-value="draft.targetModes[id] ?? 'default'"
            :disabled="props.disabled"
            @update:model-value="setTargetMode(id, $event)"
          />
        </div>
      </div>
    </section>

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
