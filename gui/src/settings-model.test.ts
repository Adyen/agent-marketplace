import { describe, expect, it } from 'vitest'
import { defaultSettings } from './fixtures'
import { draftFromSettings, settingsDraftErrors, settingsRequest } from './settings-model'

const base = draftFromSettings(defaultSettings)

describe('settings model', () => {
  // spec: gui-configuration/settings-save-writes-the-user-yaml
  it('sends only changed fields as a save-settings patch', () => {
    const draft = { ...base, backupMaxDays: '5', logLevel: 'warn' }
    expect(settingsRequest('backups', base, draft, new Set())).toEqual({
      action: 'save-settings',
      backup: { maxDays: 5 },
    })
    expect(settingsRequest('logging', base, draft, new Set())).toEqual({
      action: 'save-settings',
      logging: { level: 'warn' },
    })
  })

  it('returns null when nothing changed', () => {
    expect(settingsRequest('backups', base, base, new Set())).toBeNull()
  })

  it('sends null for a reset user-set field', () => {
    const draft = { ...base, updateCheck: false }
    expect(settingsRequest('general', base, draft, new Set(['updateCheck']))).toEqual({
      action: 'save-settings',
      updateCheck: { enabled: null },
    })
  })

  it('sends the whole override map and null when cleared', () => {
    const draft = { ...base, targetModes: { devin: 'file' as const } }
    expect(settingsRequest('install', base, draft, new Set())).toEqual({
      action: 'save-settings',
      materialization: { targets: { devin: 'file' } },
    })
    const reset = { ...draft, installMode: 'symlink' as const, targetModes: {} }
    expect(settingsRequest('install', draft, reset, new Set(['installMode', 'targetModes']))).toEqual({
      action: 'save-settings',
      materialization: { mode: null, targets: null },
    })
  })

  it('sends telemetry level with no null option', () => {
    const draft = { ...base, telemetryLevel: 'basic' as const }
    expect(settingsRequest('general', base, draft, new Set())).toEqual({
      action: 'save-settings',
      telemetry: { level: 'basic' },
    })
  })

  // spec: gui-configuration/save-refuses-an-invalid-value-without-writing
  it('reports invalid numeric values before any request', () => {
    expect(settingsDraftErrors('backups', { ...base, backupMaxDays: '-1' })).toEqual({
      backupMaxDays: 'Enter a whole number of days, 0 or more.',
    })
    expect(settingsDraftErrors('logging', { ...base, logMaxSizeMB: '0' })).toEqual({
      logMaxSizeMB: 'Enter a whole number of MB, 1 or more.',
    })
    expect(settingsDraftErrors('logging', base)).toEqual({})
  })
})
