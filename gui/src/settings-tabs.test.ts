// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import type { Configuration } from './api/client'
import { defaultSettings } from './fixtures'
import { configuration, event, fakeClient, mountWorkspace, snapshot, tick } from './test-support'

type Settings = Configuration['settings']

async function openSettingsTab(tab: string, settings: Settings = defaultSettings) {
  const { client, push } = fakeClient({ configuration: configuration({ settings, userTargets: ['devin', 'codex'] }) })
  const { wrapper } = mountWorkspace(client)
  push(event({ sequence: 1, payload: snapshot([]) }))
  await tick()
  wrapper.get('button[aria-label="Settings"]').trigger('click')
  await tick()
  document.body.querySelector<HTMLElement>(`#settings-tab-${tab}`)!.click()
  await tick()
  return { client }
}

function panel(id: string): HTMLElement {
  return document.body.querySelector<HTMLElement>(`#settings-panel-${id}`)!
}

function labelled<T extends Element = HTMLInputElement>(root: ParentNode, label: string): T {
  const el = root.querySelector<T>(`[aria-label="${label}"]`)
  if (el == null) {
    throw new Error(`no control labelled ${label}`)
  }
  return el
}

async function setValue(el: HTMLInputElement | HTMLSelectElement, value: string) {
  el.value = value
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input'))
  await tick()
}

function saveButton(root: HTMLElement): HTMLButtonElement {
  return root.querySelector<HTMLButtonElement>('button[aria-label="Save changes"]')!
}

describe('settings tabs', () => {
  // spec: gui-configuration/general-saves-the-update-check
  it('General renders the update check switch from the payload and saves a changed value', async () => {
    const { client } = await openSettingsTab('general')
    const general = panel('general')
    const updates = labelled(general, 'Show when a new release is available')
    expect(updates.checked).toBe(true)
    expect(saveButton(general).disabled).toBe(true)

    updates.click()
    await tick()
    expect(saveButton(general).disabled).toBe(false)
    saveButton(general).click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'save-settings', updateCheck: { enabled: false } })
  })

  // spec: gui-configuration/locked-telemetry-is-disabled-with-organization-copy
  it('General disables a telemetry level the organization locks', async () => {
    const settings: Settings = {
      ...defaultSettings,
      telemetry: { level: { value: 'off', default: null, source: 'organization', locked: true } },
    }
    await openSettingsTab('general', settings)
    const general = panel('general')
    expect(general.textContent).toContain('Set by your organization')
    expect(labelled(general, "Don't share").hasAttribute('disabled')).toBe(true)
  })

  // spec: gui-configuration/install-mode-saves-the-mode-and-overrides
  it('Install mode saves the mode and a per-agent override', async () => {
    const { client } = await openSettingsTab('install')
    const install = panel('install')
    expect(labelled(install, 'Symlinks').checked).toBe(true)

    await setValue(labelled<HTMLSelectElement>(install, 'Install mode for codex'), 'file')
    saveButton(install).click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({
      action: 'save-settings',
      materialization: { targets: { codex: 'file' } },
    })
  })

  // spec: gui-configuration/backups-tab-blocks-invalid-day-counts
  it('Backups saves a changed day count and blocks an invalid one', async () => {
    const { client } = await openSettingsTab('backups')
    const backups = panel('backups')
    const days = labelled(backups, 'Keep backups for N days')
    expect(days.value).toBe('14')

    await setValue(days, '-1')
    expect(backups.textContent).toContain('Enter a whole number of days, 0 or more.')
    expect(saveButton(backups).disabled).toBe(true)

    await setValue(days, '7')
    saveButton(backups).click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'save-settings', backup: { maxDays: 7 } })
  })

  // spec: gui-configuration/logging-reset-clears-the-user-value
  it('Logging resets a user-set field to null', async () => {
    const settings: Settings = {
      ...defaultSettings,
      logging: {
        ...defaultSettings.logging,
        level: { value: 'warn', default: 'info', source: 'user', locked: false },
      },
    }
    const { client } = await openSettingsTab('logging', settings)
    const logging = panel('logging')
    expect(logging.querySelector('button[aria-label="Reset Log level"]')).not.toBeNull()

    logging.querySelector<HTMLButtonElement>('button[aria-label="Reset Log level"]')!.click()
    await tick()
    saveButton(logging).click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'save-settings', logging: { level: null } })
  })
})
