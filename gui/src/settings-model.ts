import type { ConfigureRequest, Configuration } from './api/client'

export type SettingsSection = 'general' | 'install' | 'backups' | 'logging'
export type TelemetryChoice = 'off' | 'basic' | 'detailed'
export type InstallMode = 'symlink' | 'file'

export interface SettingsDraft {
  updateCheck: boolean
  telemetryLevel: TelemetryChoice
  installMode: InstallMode
  targetModes: Record<string, InstallMode>
  backupEnabled: boolean
  backupMaxDays: string
  backupMaxPerDay: string
  logEnabled: boolean
  logLevel: string
  logFile: string
  logMaxSizeMB: string
  logMaxBackups: string
  logMaxAgeDays: string
  logCompress: boolean
}

export type SettingsDraftKey = keyof SettingsDraft

type Settings = Configuration['settings']
type Patch = Record<string, Record<string, unknown>>
export type SettingsRequest = Extract<ConfigureRequest, { action: 'save-settings' }>

interface FieldSpec {
  key: SettingsDraftKey
  group: string
  field: string
  kind: 'bool' | 'int' | 'text' | 'mode' | 'targets' | 'telemetry'
  resettable: boolean
}

const SECTION_FIELDS: Record<SettingsSection, FieldSpec[]> = {
  general: [
    { key: 'updateCheck', group: 'updateCheck', field: 'enabled', kind: 'bool', resettable: true },
    { key: 'telemetryLevel', group: 'telemetry', field: 'level', kind: 'telemetry', resettable: false },
  ],
  install: [
    { key: 'installMode', group: 'materialization', field: 'mode', kind: 'mode', resettable: true },
    { key: 'targetModes', group: 'materialization', field: 'targets', kind: 'targets', resettable: true },
  ],
  backups: [
    { key: 'backupEnabled', group: 'backup', field: 'enabled', kind: 'bool', resettable: true },
    { key: 'backupMaxDays', group: 'backup', field: 'maxDays', kind: 'int', resettable: true },
    { key: 'backupMaxPerDay', group: 'backup', field: 'maxPerDay', kind: 'int', resettable: true },
  ],
  logging: [
    { key: 'logEnabled', group: 'logging', field: 'enabled', kind: 'bool', resettable: true },
    { key: 'logLevel', group: 'logging', field: 'level', kind: 'text', resettable: true },
    { key: 'logFile', group: 'logging', field: 'file', kind: 'text', resettable: true },
    { key: 'logMaxSizeMB', group: 'logging', field: 'maxSizeMB', kind: 'int', resettable: true },
    { key: 'logMaxBackups', group: 'logging', field: 'maxBackups', kind: 'int', resettable: true },
    { key: 'logMaxAgeDays', group: 'logging', field: 'maxAgeDays', kind: 'int', resettable: true },
    { key: 'logCompress', group: 'logging', field: 'compress', kind: 'bool', resettable: true },
  ],
}

const INT_MINIMUM: Partial<Record<SettingsDraftKey, { min: number; message: string }>> = {
  backupMaxDays: { min: 0, message: 'Enter a whole number of days, 0 or more.' },
  backupMaxPerDay: { min: 0, message: 'Enter a whole number, 0 or more.' },
  logMaxSizeMB: { min: 1, message: 'Enter a whole number of MB, 1 or more.' },
  logMaxBackups: { min: 0, message: 'Enter a whole number, 0 or more.' },
  logMaxAgeDays: { min: 0, message: 'Enter a whole number of days, 0 or more.' },
}

type Field = { value: unknown; default: unknown }
type Pick = 'value' | 'default'

function valueOf<T>(field: Field | undefined | null, pick: Pick): T {
  return field?.[pick] as T
}

function numberText(value: unknown): string {
  return value == null ? '' : String(value)
}

function targetModes(map: unknown): Record<string, InstallMode> {
  return { ...((map ?? {}) as Record<string, InstallMode>) }
}

// draftFromSettings reads each field's effective value, or its built-in
// default when pick is 'default'.
export function draftFromSettings(settings: Settings, pick: Pick = 'value'): SettingsDraft {
  const v = <T,>(field: Field) => valueOf<T>(field, pick)
  return {
    updateCheck: v<boolean>(settings.updateCheck.enabled),
    telemetryLevel: v<TelemetryChoice | null>(settings.telemetry.level) ?? 'off',
    installMode: v<InstallMode>(settings.materialization.mode),
    targetModes: targetModes(v(settings.materialization.targets)),
    backupEnabled: v<boolean>(settings.backup.enabled),
    backupMaxDays: numberText(v(settings.backup.maxDays)),
    backupMaxPerDay: numberText(v(settings.backup.maxPerDay)),
    logEnabled: v<boolean>(settings.logging.enabled),
    logLevel: v<string>(settings.logging.level),
    logFile: v<string | null>(settings.logging.file) ?? '',
    logMaxSizeMB: numberText(v(settings.logging.maxSizeMB)),
    logMaxBackups: numberText(v(settings.logging.maxBackups)),
    logMaxAgeDays: numberText(v(settings.logging.maxAgeDays)),
    logCompress: v<boolean>(settings.logging.compress),
  }
}

export function settingsDraftErrors(section: SettingsSection, draft: SettingsDraft): Partial<Record<SettingsDraftKey, string>> {
  const errors: Partial<Record<SettingsDraftKey, string>> = {}
  for (const spec of SECTION_FIELDS[section]) {
    const rule = INT_MINIMUM[spec.key]
    if (rule == null) {
      continue
    }
    const text = draft[spec.key] as string
    if (!/^\d+$/.test(text) || Number(text) < rule.min) {
      errors[spec.key] = rule.message
    }
  }
  return errors
}

function sameValue(spec: FieldSpec, base: SettingsDraft, draft: SettingsDraft): boolean {
  return JSON.stringify(base[spec.key]) === JSON.stringify(draft[spec.key])
}

function encodeValue(spec: FieldSpec, draft: SettingsDraft): unknown {
  const raw = draft[spec.key]
  switch (spec.kind) {
    case 'int':
      return Number(raw)
    case 'text':
      return typeof raw === 'string' && raw.trim() !== '' ? raw.trim() : null
    case 'targets': {
      const map = raw as Record<string, InstallMode>
      return Object.keys(map).length === 0 ? null : map
    }
    default:
      return raw
  }
}

// settingsRequest builds the save-settings patch for one section. It sends
// changed fields, sends null for fields the user reset to their default, and
// returns null when nothing changed.
export function settingsRequest(
  section: SettingsSection,
  base: SettingsDraft,
  draft: SettingsDraft,
  resets: ReadonlySet<SettingsDraftKey>,
): SettingsRequest | null {
  const patch: Patch = {}
  for (const spec of SECTION_FIELDS[section]) {
    const reset = spec.resettable && resets.has(spec.key)
    if (!reset && sameValue(spec, base, draft)) {
      continue
    }
    patch[spec.group] ??= {}
    patch[spec.group][spec.field] = reset ? null : encodeValue(spec, draft)
  }
  if (Object.keys(patch).length === 0) {
    return null
  }
  return { action: 'save-settings', ...patch } as SettingsRequest
}
