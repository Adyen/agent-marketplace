// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import { closeRequestEvent } from './bootstrap'
import { RequestError, type OperationRef } from './api/client'
import Confirmations from './Confirmations.vue'
import { WorkspaceController } from './controller'
import { event, fakeClient, mountWorkspace, row, rowNames, snapshot, tick } from './test-support'

function snapshotEvent(sequence: number, overrides: Parameters<typeof snapshot>[1] = {}, generation = 1) {
  return event({ sequence, generation, payload: snapshot([row('solo')], overrides) })
}

function operationSnapshot(sequence: number, operation: OperationRef, generation = 1, overrides: Parameters<typeof snapshot>[1] = {}) {
  return event({ sequence, generation, operation, payload: snapshot([row('solo')], { operation, ...overrides }) })
}

function settledSnapshot(sequence: number, settled: OperationRef, overrides: Parameters<typeof snapshot>[1] = {}, generation = 1) {
  return event({
    sequence,
    generation,
    payload: snapshot([row('solo')], { settledOperation: settled, ...overrides }),
  })
}

function dialog(): HTMLElement | null {
  return document.body.querySelector('[role="dialog"]')
}

function dialogButton(label: string): HTMLElement | undefined {
  return Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"] button')).find(
    (b) => b.getAttribute('aria-label') === label,
  )
}

describe('quit confirmation', () => {
  function quitMount(pendingChanges = 0) {
    const { client, push } = fakeClient()
    const onQuit = vi.fn()
    const mounted = mountWorkspace(client, 60_000, onQuit)
    push(snapshotEvent(1, { pendingChanges }))
    return { client, push, onQuit, ...mounted }
  }

  // spec: session-interactions/confirmation-retains-all-user-choices
  // spec: gui-shell/quit-with-nothing-pending-exits-immediately
  it('quit with nothing pending exits immediately without a confirmation or a sync', async () => {
    const { wrapper, onQuit, client } = quitMount(0)
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    expect(dialog()).toBeNull()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(client.action).not.toHaveBeenCalled()
    expect(client.telemetry).not.toHaveBeenCalled()
  })

  // spec: gui-shell/quit-with-pending-or-running-work-confirms
  it('quit while a non-sync operation runs offers Cancel, Stay, and Quit', async () => {
    const { wrapper, onQuit, client, push } = quitMount(0)
    push(operationSnapshot(2, { id: 7, kind: 'update' }, 2))
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    expect(dialog()?.textContent).toContain('Changes are still being applied. Quit anyway?')
    expect(dialogButton('Stay')).toBeDefined()
    expect(dialogButton('Cancel quit')).toBeDefined()

    dialogButton('Cancel quit')!.click()
    await tick()
    expect(onQuit).not.toHaveBeenCalled()
    expect(client.action).not.toHaveBeenCalled()
    expect(dialog()).toBeNull()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Quit now')!.click()
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(client.telemetry).not.toHaveBeenCalled()
  })

  // spec: gui-shell/quit-with-pending-or-running-work-confirms
  it('quit before the session finishes loading explains that unsynced changes are still being checked', async () => {
    const { client, push } = fakeClient()
    const onQuit = vi.fn()
    const { wrapper } = mountWorkspace(client, 60_000, onQuit)
    push(event({ sequence: 1, payload: snapshot([row('solo')], { stage: 'config', pendingChanges: 0 }) }))
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    expect(dialog()?.textContent).toContain('Still checking for unsynced changes. Quit anyway?')
    expect(dialog()?.textContent).not.toContain('Changes are still being applied')
    expect(dialogButton('Stay')).toBeDefined()
    expect(dialogButton('Cancel quit')).toBeDefined()
    expect(dialogButton('Quit now')).toBeDefined()

    dialogButton('Stay')!.click()
    await tick()
    expect(onQuit).not.toHaveBeenCalled()
    expect(client.action).not.toHaveBeenCalled()
  })

  // spec: gui-shell/quit-with-nothing-pending-exits-immediately
  it('native close request with nothing pending exits without opening the confirmation', async () => {
    const listeners: Record<string, (() => void) | undefined> = {}
    const previousRuntime = window.runtime
    window.runtime = {
      EventsOn: (event: string, callback: () => void) => {
        listeners[event] = callback
        return () => {
          delete listeners[event]
        }
      },
    }
    try {
      const { onQuit } = quitMount(0)
      await tick()
      expect(listeners[closeRequestEvent]).toBeDefined()
      listeners[closeRequestEvent]?.()
      await tick()
      expect(dialog()).toBeNull()
      expect(onQuit).toHaveBeenCalledTimes(1)
    } finally {
      window.runtime = previousRuntime
    }
  })

  // spec: gui-shell/quit-with-pending-or-running-work-confirms
  it('native close request with pending changes opens the confirmation and Stay keeps the app open', async () => {
    const listeners: Record<string, (() => void) | undefined> = {}
    const previousRuntime = window.runtime
    window.runtime = {
      EventsOn: (event: string, callback: () => void) => {
        listeners[event] = callback
        return () => {
          delete listeners[event]
        }
      },
    }
    try {
      const { onQuit } = quitMount(2)
      await tick()
      listeners[closeRequestEvent]?.()
      await tick()
      expect(dialogButton('Sync and quit')).toBeDefined()
      dialogButton('Cancel quit')!.click()
      await tick()
      expect(onQuit).not.toHaveBeenCalled()
      expect(dialog()).toBeNull()
    } finally {
      window.runtime = previousRuntime
    }
  })

  it('Escape dismissal records one cancellation when sync choices were offered', async () => {
    const { client, wrapper, onQuit } = quitMount(1)
    await tick()
    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    expect(dialogButton('Sync and quit')).toBeDefined()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await tick()

    expect(dialog()).toBeNull()
    expect(onQuit).not.toHaveBeenCalled()
    expect(client.action).not.toHaveBeenCalled()
    expect(client.telemetry).toHaveBeenCalledTimes(1)
    expect(client.telemetry).toHaveBeenCalledWith({ action: 'modal_outcome', modal: 'sync+quit', outcome: 'cancelled' })
  })

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('tracks sync choices first offered while the quit dialog is open', async () => {
    const { client, push, wrapper } = quitMount(0)
    await tick()
    push(operationSnapshot(2, { id: 7, kind: 'update' }, 2))
    await tick()
    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    expect(dialogButton('Sync and quit')).toBeUndefined()

    push(snapshotEvent(3, { pendingChanges: 1 }, 2))
    await tick()
    expect(dialogButton('Sync and quit')).toBeDefined()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await tick()

    expect(dialog()).toBeNull()
    expect(client.telemetry).toHaveBeenCalledTimes(1)
    expect(client.telemetry).toHaveBeenCalledWith({ action: 'modal_outcome', modal: 'sync+quit', outcome: 'cancelled' })
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('pending changes offer sync-and-quit which waits for the matching settled sync', async () => {
    const { client, push, wrapper, onQuit, controller } = quitMount(2)
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).toHaveBeenCalledTimes(1)
    expect(client.action).toHaveBeenCalledWith('sync')

    push(
      event({
        sequence: 2,
        generation: 2,
        operation: { id: 7, kind: 'sync' },
        payload: snapshot([row('solo')], { stage: 'inventory', operation: { id: 7, kind: 'sync' }, pendingChanges: 2 }),
      }),
    )
    await tick()
    expect(controller.state.operation?.id).toBe(7)
    expect(onQuit).not.toHaveBeenCalled()

    push(
      event({
        sequence: 3,
        generation: 2,
        type: 'progress',
        operation: { id: 7, kind: 'sync' },
        payload: { progressSequence: 1, phase: 'materializing' },
      }),
    )
    await tick()
    expect(controller.state.phase).toBe('materializing')
    expect(onQuit).not.toHaveBeenCalled()

    push(
      event({
        sequence: 4,
        generation: 2,
        operation: { id: 7, kind: 'sync' },
        payload: snapshot([row('solo')], { stage: 'inventory', operation: { id: 7, kind: 'sync' }, pendingChanges: 2 }),
      }),
    )
    await tick()
    expect(controller.state.operation?.id).toBe(7)
    expect(onQuit).not.toHaveBeenCalled()

    push(settledSnapshot(5, { id: 7, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(controller.state.operation).toBeNull()
    expect(client.telemetry).not.toHaveBeenCalled()
  })

  // spec: session-interactions/cancel-or-stale-completion
  it('an unrelated or stale settlement never quits', async () => {
    const { client, push, wrapper, onQuit } = quitMount(1)
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).toHaveBeenCalledWith('sync')

    push(settledSnapshot(2, { id: 6, kind: 'sync' }, {}, 2))
    push(settledSnapshot(3, { id: 9, kind: 'update' }, {}, 2))
    await tick()
    expect(onQuit).not.toHaveBeenCalled()

    push(settledSnapshot(4, { id: 7, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('attaches quit to an already running sync without starting a duplicate', async () => {
    const { client, push, wrapper, onQuit } = quitMount(1)
    await tick()
    push(operationSnapshot(2, { id: 11, kind: 'sync' }, 2, { pendingChanges: 1 }))
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).not.toHaveBeenCalled()

    push(settledSnapshot(3, { id: 11, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
  })

  // spec: session-interactions/guarded-submission
  it('blocks sync-and-quit while another operation holds the serializer', async () => {
    const { client, push, wrapper, onQuit } = quitMount(1)
    await tick()
    push(operationSnapshot(2, { id: 11, kind: 'update' }, 2, { pendingChanges: 1 }))
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    const syncQuit = dialogButton('Sync and quit')! as HTMLButtonElement
    expect(syncQuit.disabled).toBe(true)

    dialogButton('Quit without syncing')!.click()
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(client.action).not.toHaveBeenCalled()
    expect(client.telemetry).toHaveBeenCalledTimes(1)
    expect(client.telemetry).toHaveBeenCalledWith({ action: 'modal_outcome', modal: 'sync+quit', outcome: 'cancelled' })
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('quits exactly once when the sync response arrives after the stream final', async () => {
    const { client, push, onQuit, controller } = quitMount(1)
    await tick()
    let resolveAction: (operation: OperationRef) => void = () => {}
    client.action = vi.fn(
      () =>
        new Promise<OperationRef>((resolve) => {
          resolveAction = resolve
        }),
    )
    const pending = controller.syncAndQuit()
    push(operationSnapshot(2, { id: 9, kind: 'sync' }, 2))
    push(settledSnapshot(3, { id: 9, kind: 'sync' }, {}, 2))
    await tick()
    resolveAction({ id: 9, kind: 'sync' })
    expect(await pending).toBe(true)
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('still exits after an explicitly requested sync that failed', async () => {
    const { client, push } = fakeClient()
    let controller!: WorkspaceController
    const observed: { stage: string; drift: string; operation: OperationRef | null; messages: (string | undefined)[] }[] = []
    const mounted = mountWorkspace(client, 60_000, () => {
      observed.push({
        stage: controller.state.stage,
        drift: controller.state.drift,
        operation: controller.state.operation,
        messages: controller.state.diagnostics.map((d) => d.message),
      })
    })
    controller = mounted.controller
    const { wrapper } = mounted
    push(snapshotEvent(1, { pendingChanges: 1 }))
    await tick()
    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).toHaveBeenCalledWith('sync')

    push(
      settledSnapshot(
        2,
        { id: 7, kind: 'sync' },
        { diagnostics: [{ severity: 'error', category: 'sync', code: 'sync.failed', message: 'sync broke' }] },
        2,
      ),
    )
    await tick()
    expect(observed).toHaveLength(1)
    expect(observed[0].stage).toBe('final')
    expect(observed[0].drift).toBe('ready')
    expect(observed[0].operation).toBeNull()
    expect(observed[0].messages).toContain('sync broke')
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('cancel quit during a pending sync request prevents the quit', async () => {
    const { client, push, wrapper, onQuit } = quitMount(1)
    await tick()
    let resolveAction: (operation: OperationRef) => void = () => {}
    client.action = vi.fn(
      () =>
        new Promise<OperationRef>((resolve) => {
          resolveAction = resolve
        }),
    )

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).toHaveBeenCalledWith('sync')

    const cancel = dialogButton('Cancel quit')! as HTMLButtonElement
    expect(cancel.disabled).toBe(false)
    cancel.click()
    await tick()
    expect(client.telemetry).toHaveBeenCalledTimes(1)
    expect(client.telemetry).toHaveBeenCalledWith({ action: 'modal_outcome', modal: 'sync+quit', outcome: 'cancelled' })

    resolveAction({ id: 9, kind: 'sync' })
    await tick()
    push(settledSnapshot(2, { id: 9, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).not.toHaveBeenCalled()
    expect(client.telemetry).toHaveBeenCalledTimes(1)
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('cancelling a requested sync-and-quit from a later close request clears the intent', async () => {
    const { client, push, wrapper, onQuit, controller } = quitMount(1)
    await tick()
    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).toHaveBeenCalledWith('sync')
    expect(dialog()).toBeNull()

    push(operationSnapshot(2, { id: 7, kind: 'sync' }, 2, { pendingChanges: 1 }))
    await tick()
    controller.cancelQuit()
    push(settledSnapshot(3, { id: 7, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).not.toHaveBeenCalled()
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('dispose during a pending sync request prevents the quit', async () => {
    const { client, push, onQuit, controller } = quitMount(1)
    await tick()
    let resolveAction: (operation: OperationRef) => void = () => {}
    client.action = vi.fn(
      () =>
        new Promise<OperationRef>((resolve) => {
          resolveAction = resolve
        }),
    )
    const pending = controller.syncAndQuit()
    push(operationSnapshot(2, { id: 9, kind: 'sync' }, 2))
    push(settledSnapshot(3, { id: 9, kind: 'sync' }, {}, 2))
    await tick()

    controller.dispose()
    resolveAction({ id: 9, kind: 'sync' })
    await pending
    await tick()
    expect(onQuit).not.toHaveBeenCalled()
    expect(client.telemetry).not.toHaveBeenCalled()
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('explicit quit while attached to a running sync leaves no intent for its final', async () => {
    const { client, push, wrapper, onQuit } = quitMount(1)
    await tick()
    push(operationSnapshot(2, { id: 11, kind: 'sync' }, 2, { pendingChanges: 1 }))
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Quit without syncing')!.click()
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(client.telemetry).toHaveBeenCalledTimes(1)
    expect(client.telemetry).toHaveBeenCalledWith({ action: 'modal_outcome', modal: 'sync+quit', outcome: 'cancelled' })

    push(settledSnapshot(3, { id: 11, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(client.action).not.toHaveBeenCalled()
    expect(client.telemetry).toHaveBeenCalledTimes(1)
  })

  // spec: session-interactions/cancel-or-stale-completion
  it('an unrelated settlement while another operation is active does not finish it or replace diagnostics', async () => {
    const { client, push, wrapper, onQuit, controller } = quitMount(1)
    await tick()
    push(operationSnapshot(2, { id: 7, kind: 'sync' }, 2, { pendingChanges: 1 }))
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).not.toHaveBeenCalled()

    push(
      settledSnapshot(
        3,
        { id: 9, kind: 'update' },
        { diagnostics: [{ severity: 'error', category: 'update', code: 'update.failed', message: 'unrelated failure' }] },
        2,
      ),
    )
    await tick()
    expect(onQuit).not.toHaveBeenCalled()
    expect(controller.state.operation?.id).toBe(7)
    expect(controller.state.diagnostics.map((d) => d.message)).not.toContain('unrelated failure')

    push(settledSnapshot(4, { id: 7, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(controller.state.operation).toBeNull()
  })

  // spec: session-interactions/cancel-or-stale-completion
  it('a settlement older than the last accepted one is ignored', async () => {
    const { client, push, wrapper, onQuit, controller } = quitMount(1)
    await tick()
    push(operationSnapshot(2, { id: 9, kind: 'sync' }, 2, { pendingChanges: 1 }))
    await tick()

    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()
    expect(client.action).not.toHaveBeenCalled()

    push(
      settledSnapshot(
        3,
        { id: 9, kind: 'sync' },
        { diagnostics: [{ severity: 'warning', category: 'sync', code: 'sync.done', message: 'final assessment' }] },
        2,
      ),
    )
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(controller.state.diagnostics.map((d) => d.message)).toContain('final assessment')

    push(
      settledSnapshot(
        4,
        { id: 7, kind: 'sync' },
        { diagnostics: [{ severity: 'error', category: 'sync', code: 'sync.stale', message: 'stale failure' }] },
        2,
      ),
    )
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(controller.state.diagnostics.map((d) => d.message)).toContain('final assessment')
    expect(controller.state.diagnostics.map((d) => d.message)).not.toContain('stale failure')
    expect(controller.state.operation).toBeNull()
  })

  // spec: session-interactions/cancel-or-stale-completion
  it('a late announcement for an already settled operation does not reactivate it', async () => {
    const { client, push, wrapper, onQuit, controller } = quitMount(1)
    await tick()
    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()

    push(settledSnapshot(2, { id: 7, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(controller.state.operation).toBeNull()

    push(operationSnapshot(3, { id: 7, kind: 'sync' }, 3, { pendingChanges: 1 }))
    await tick()
    expect(controller.state.operation).toBeNull()

    push(settledSnapshot(4, { id: 7, kind: 'sync' }, {}, 3))
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(onQuit).toHaveBeenCalledTimes(1)

    push(operationSnapshot(5, { id: 9, kind: 'sync' }, 3, { pendingChanges: 1 }))
    await tick()
    expect(controller.state.operation?.id).toBe(9)
  })

  // spec: session-interactions/confirmation-retains-all-user-choices
  it('repeated final frames for the last settled operation still apply rows without quitting again', async () => {
    const { client, push, wrapper, onQuit, controller } = quitMount(1)
    await tick()
    await wrapper.find('button[aria-label="Quit"]').trigger('click')
    await tick()
    dialogButton('Sync and quit')!.click()
    await tick()

    push(settledSnapshot(2, { id: 7, kind: 'sync' }, {}, 2))
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    expect(controller.state.rows.map((r) => r.name)).toContain('solo')

    push(
      event({
        sequence: 3,
        generation: 3,
        payload: snapshot([row('reloaded')], {
          settledOperation: { id: 7, kind: 'sync' },
          drift: 'ready',
          diagnostics: [{ severity: 'warning', category: 'sync', code: 'sync.reload', message: 'reload assessment' }],
        }),
      }),
    )
    await tick()
    expect(onQuit).toHaveBeenCalledTimes(1)
    const names = controller.state.rows.map((r) => r.name)
    expect(names).toContain('reloaded')
    expect(names).not.toContain('solo')
    expect(controller.state.diagnostics.map((d) => d.message)).toContain('reload assessment')
    expect(controller.state.drift).toBe('ready')
  })
})

// spec: gui-shell/configuration-actions-do-not-open-a-raw-editor
describe('no raw configuration editor', () => {
  it('no action or keyboard trigger opens a raw editor', async () => {
    const { client, push } = fakeClient()
    const openSpy = vi.spyOn(window, 'open')
    const execSpy = vi.fn()
    const hadExecCommand = 'execCommand' in document
    const previousExecCommand = (document as unknown as { execCommand?: unknown }).execCommand
    ;(document as unknown as { execCommand?: unknown }).execCommand = execSpy
    try {
      const { wrapper } = mountWorkspace(client)
      push(snapshotEvent(1, { pendingChanges: 2 }))
      await tick()

      const toolbar = wrapper.findAll('.workspace__actions button')
      expect(toolbar.length).toBeGreaterThan(0)
      for (const button of toolbar) {
        await button.trigger('keydown', { key: 'e' })
        await button.trigger('keydown', { key: 'E' })
      }
      await wrapper.find('[role="listbox"]').trigger('keydown', { key: 'e' })
      await wrapper.find('[role="listbox"]').trigger('keydown', { key: 'E' })

      for (const label of ['Settings', 'Sources']) {
        await wrapper.find(`button[aria-label="${label}"]`).trigger('click')
        await tick()
        const open = dialog()
        expect(open).not.toBeNull()
        for (const el of Array.from(open!.querySelectorAll<HTMLElement>('button, input, [role="combobox"]'))) {
          el.dispatchEvent(new KeyboardEvent('keydown', { key: 'e', bubbles: true }))
          el.dispatchEvent(new KeyboardEvent('keydown', { key: 'E', bubbles: true }))
        }
        await nextTick()
        open!.querySelector<HTMLElement>('button[aria-label="Close dialog"]')!.click()
        await tick()
        expect(dialog()).toBeNull()
      }

      expect(document.body.querySelectorAll('textarea')).toHaveLength(0)
      expect(document.body.querySelectorAll('[contenteditable="true"]')).toHaveLength(0)
      expect(openSpy).not.toHaveBeenCalled()
      expect(execSpy).not.toHaveBeenCalled()
      expect(client.configure).not.toHaveBeenCalled()
      const actions = (client.action as ReturnType<typeof vi.fn>).mock.calls.map(([arg]) => arg)
      expect(actions).not.toContain('editor')
    } finally {
      openSpy.mockRestore()
      if (hadExecCommand) {
        ;(document as unknown as { execCommand?: unknown }).execCommand = previousExecCommand
      } else {
        delete (document as unknown as { execCommand?: unknown }).execCommand
      }
    }
  })
})
