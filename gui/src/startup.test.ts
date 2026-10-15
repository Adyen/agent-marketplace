// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import type { OperationRef, SessionEvent, Snapshot } from './api/client'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

const owned = row('owned', {
  id: 'opaque-owned',
  registry: 'local',
  scope: 'user',
  checked: true,
  group: 'pending',
  status: 'pending sync',
  pendingVerification: false,
  resourceAction: 'remove',
  description: 'Verified owned detail',
})

const second = row('second', {
  id: 'opaque-second',
  registry: 'local',
  scope: 'available',
  checked: false,
  group: 'available',
  status: 'available',
  pendingVerification: false,
  resourceAction: 'install',
})

const pendingOwned = row('owned', {
  id: 'opaque-owned',
  registry: 'local',
  scope: 'user',
  checked: true,
  group: 'pending',
  status: 'pending verification',
  pendingVerification: true,
  resourceAction: 'none',
})

const newPending = row('newpending', {
  id: 'opaque-newpending',
  registry: 'local',
  scope: 'user',
  checked: false,
  group: 'pending',
  status: 'pending verification',
  pendingVerification: true,
  resourceAction: 'none',
})

const newVerified = row('newverified', {
  id: 'opaque-newverified',
  registry: 'local',
  scope: 'available',
  checked: false,
  group: 'available',
  status: 'available',
  pendingVerification: false,
  resourceAction: 'install',
  description: 'Verified replacement detail',
})

const assessmentWarning = {
  category: 'resolution',
  code: 'assessment.fixture',
  message: 'Assessment fixture warning',
  severity: 'warning' as const,
}

function frame(
  sequence: number,
  generation: number,
  stage: Snapshot['stage'],
  rows: ReturnType<typeof row>[],
  extras: Partial<Snapshot> = {},
): SessionEvent {
  const payload = snapshot(rows, {
    stage,
    drift: stage === 'final' ? 'ready' : 'pending',
    ...extras,
  })
  return event({ sequence, generation, operation: extras.operation ?? null, payload })
}

function selectedOptionID(wrapper: ReturnType<typeof mountWorkspace>['wrapper']): string {
  const id = wrapper.find('[role="option"][aria-selected="true"]').attributes('id')
  if (id == null) throw new Error('startup consumer had no selected option DOM id')
  return id
}

function useProvisionalInitialSnapshot(client: ReturnType<typeof fakeClient>['client']): void {
  vi.mocked(client.snapshot).mockResolvedValue(snapshot([], { stage: 'provisional', drift: 'pending', harnesses: ['devin'] }))
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise })
  return { promise, resolve }
}

describe('startup stage consumers', () => {
  // spec: session-startup/superseded-stage-delivery-cannot-overwrite-newer-state
  // spec: session-startup/a-stale-provisional-stage-cannot-publish-rows
  it('ignores stale generation stages after newer inventory has been accepted', async () => {
    const { client, push } = fakeClient()
    useProvisionalInitialSnapshot(client)
    const { controller, wrapper } = mountWorkspace(client)
    push(frame(1, 7, 'config', [], { harnesses: ['devin'], discoveryEnabled: false }))
    await tick()
    expect(controller.state.stage).toBe('config')

    push(frame(2, 8, 'inventory', [owned, second], { harnesses: ['devin'], pendingChanges: 1 }))
    await tick()
    await wrapper.get(`[role="option"][id="row-${owned.id}"]`).trigger('click')
    await tick()
    const selectedDOMID = selectedOptionID(wrapper)
    const priorRows = controller.state.rows.map((candidate) => ({ ...candidate }))
    const priorState = {
      stage: controller.state.stage,
      drift: controller.state.drift,
      diagnostics: controller.state.diagnostics.map((diagnostic) => ({ ...diagnostic })),
      operation: controller.state.operation,
      discoveryEnabled: controller.state.discoveryEnabled,
      selectedText: wrapper.get(`[role="option"][id="${selectedDOMID}"]`).text(),
    }
    const staleInventory = row('stale', {
      id: 'opaque-stale-inventory',
      registry: 'local',
      scope: 'available',
      checked: false,
      group: 'available',
      status: 'available',
      resourceAction: 'install',
    })
    const stalePending = row('stale-pending', {
      id: 'opaque-stale',
      registry: 'local',
      scope: 'user',
      checked: false,
      group: 'pending',
      status: 'pending verification',
      pendingVerification: true,
      resourceAction: 'none',
    })
    const staleFrames = [
      frame(3, 7, 'config', [], { harnesses: ['devin'], discoveryEnabled: true }),
      frame(4, 7, 'inventory', [staleInventory]),
      frame(5, 7, 'provisional', [stalePending]),
    ]
    for (const staleFrame of staleFrames) {
      push(staleFrame)
      await tick()
      expect(controller.state.rows).toEqual(priorRows)
      expect(controller.state.stage).toBe(priorState.stage)
      expect(controller.state.drift).toBe(priorState.drift)
      expect(controller.state.diagnostics).toEqual(priorState.diagnostics)
      expect(controller.state.operation).toEqual(priorState.operation)
      expect(controller.state.discoveryEnabled).toBe(priorState.discoveryEnabled)
      expect(selectedOptionID(wrapper)).toBe(selectedDOMID)
      expect(wrapper.get(`[role="option"][id="${selectedDOMID}"]`).text()).toBe(priorState.selectedText)
      expect(wrapper.find(`[role="option"][id="row-${staleInventory.id}"]`).exists()).toBe(false)
      expect(wrapper.find(`[role="option"][id="row-${stalePending.id}"]`).exists()).toBe(false)
    }
    expect(wrapper.get(`[role="option"][id="row-${owned.id}"]`).text()).not.toContain('pending verification')
    expect(client.discovery).not.toHaveBeenCalled()
  })

  // spec: session-startup/mutation-actions-remain-gated-until-verified-inventory
  // spec: session-startup/verified-inventory-is-navigable-while-drift-is-pending
  // spec: session-startup/final-stage-replaces-pending-inventory-with-the-assessment
  it('gates pending startup rows until inventory and hands off the final assessment', async () => {
    const { client, push } = fakeClient()
    useProvisionalInitialSnapshot(client)
    const { controller, wrapper } = mountWorkspace(client)
    push(frame(1, 7, 'provisional', [pendingOwned], { harnesses: ['devin'] }))
    await tick()
    const pendingOption = wrapper.get(`[role="option"][id="row-${pendingOwned.id}"]`)
    const pendingDOMID = pendingOption.attributes('id')
    if (pendingDOMID == null) {
      throw new Error('pending startup row had no stable DOM id')
    }

    push(frame(2, 7, 'config', [pendingOwned], { harnesses: ['devin'] }))
    await tick()
    expect(wrapper.find(`[role="option"][id="${pendingDOMID}"]`).exists()).toBe(true)
    expect(controller.state.rows[0]?.id).toBe(pendingOwned.id)
    expect((wrapper.get('button[aria-label="Sync"]').element as HTMLButtonElement).disabled).toBe(true)
    expect((wrapper.get('button[aria-label="Update"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('button[aria-label="Remove Resource"]').exists()).toBe(false)

    push(frame(3, 7, 'inventory', [owned, second], { harnesses: ['devin'], pendingChanges: 1 }))
    await tick()
    expect(controller.state.stage).toBe('inventory')
    expect(controller.state.drift).toBe('pending')
    expect((wrapper.get('button[aria-label="Sync"]').element as HTMLButtonElement).disabled).toBe(false)
    expect((wrapper.get('button[aria-label="Update"]').element as HTMLButtonElement).disabled).toBe(false)
    const ownedOption = wrapper.get(`[role="option"][id="row-${owned.id}"]`)
    await ownedOption.trigger('click')
    await tick()
    await wrapper.get(`[role="option"][id="row-${owned.id}"]`).trigger('keydown', { key: 'Enter' })
    await tick()
    const detail = wrapper.get('aside[aria-label="Detail"]')
    expect(detail.text()).toContain('Verified owned detail')
    expect(detail.text()).toContain('pending sync')
    expect(detail.text()).toContain('Drift: pending')
    expect((wrapper.get('button[aria-label="Remove Resource"]').element as HTMLButtonElement).disabled).toBe(false)

    const finalOwned: ReturnType<typeof row> = { ...owned, group: 'enabled', status: 'enabled' }
    push(frame(4, 7, 'final', [finalOwned], { diagnostics: [assessmentWarning], pendingChanges: 0 }))
    await tick()
    expect(controller.state.stage).toBe('final')
    expect(controller.state.drift).toBe('ready')
    expect(controller.state.rows).toEqual([finalOwned])
    expect(controller.state.rows[0]?.pendingVerification).toBe(false)
    expect(controller.state.diagnostics).toEqual([assessmentWarning])
    expect(detail.text()).toContain('Assessment fixture warning')
    expect(detail.text()).toContain('enabled')
    expect(detail.text()).toContain('Drift: ready')
    expect(detail.text()).not.toContain('Drift: pending')
    expect(wrapper.text()).not.toContain('pending verification')
  })

  // spec: session-startup/an-intermediate-stage-cannot-complete-a-pending-mutation-reload
  it('keeps an accepted configuration operation active across inventory and settles only at final', async () => {
    const priorWarning = {
      category: 'resolution',
      code: 'assessment.previous',
      message: 'Previous assessment warning',
      severity: 'warning' as const,
    }
    const finalWarning = {
      category: 'resolution',
      code: 'assessment.final',
      message: 'Final assessment warning',
      severity: 'warning' as const,
    }
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(frame(1, 7, 'final', [owned, second], { diagnostics: [priorWarning] }))
    await tick()

    const configureRequest = deferred<OperationRef>()
    vi.mocked(client.configure).mockReturnValue(configureRequest.promise)
    const save = controller.saveTargets(['devin'])
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'save-targets', targets: ['devin'] })
    expect(client.action).not.toHaveBeenCalled()
    expect(controller.state.requesting).toBe(true)

    const accepted: OperationRef = { id: 91, kind: 'targets' }
    configureRequest.resolve(accepted)
    await expect(save).resolves.toBe(true)
    await tick()
    expect(controller.state.operation).toEqual(accepted)
    const acceptedPhase = controller.state.phase

    push(frame(2, 8, 'inventory', [owned], { operation: accepted, drift: 'pending', diagnostics: [priorWarning], pendingChanges: 1 }))
    await tick()
    expect(controller.state.operation).toEqual(accepted)
    expect(controller.state.phase).toBe(acceptedPhase)
    expect(controller.state.settledMutation).toBeNull()
    expect(controller.state.rows.map((candidate) => candidate.id)).toEqual([owned.id, second.id])
    expect(controller.state.diagnostics).toEqual([priorWarning])
    expect(wrapper.find('p[role="status"]').text()).toBe('targets running')
    expect((wrapper.get('button[aria-label="Sync"]').element as HTMLButtonElement).disabled).toBe(true)

    const finalOwned: ReturnType<typeof row> = { ...owned, group: 'enabled', status: 'enabled' }
    push(frame(3, 8, 'final', [finalOwned], { operation: null, settledOperation: accepted, drift: 'ready', diagnostics: [finalWarning], pendingChanges: 0 }))
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
    expect(controller.state.rows).toEqual([finalOwned])
    expect(controller.state.diagnostics).toEqual([finalWarning])
    expect(wrapper.find('p[role="status"]').exists()).toBe(false)
    expect(wrapper.find(`[role="option"][id="row-${finalOwned.id}"]`).exists()).toBe(true)
    expect(wrapper.get('aside[aria-label="Detail"]').text()).toContain('Verified owned detail')
    expect(wrapper.get('aside[aria-label="Detail"]').text()).toContain('enabled')
    expect(wrapper.get('ul[aria-label="Diagnostics"]').text()).toContain('Final assessment warning')
    expect(client.action).not.toHaveBeenCalled()
    expect(client.configure).toHaveBeenCalledTimes(1)
  })

  // spec: session-startup/authoritative-failure-clears-provisional-rows
  it('clears provisional rows and keeps guarded actions blocked after authoritative failure', async () => {
    const failure = {
      category: 'resolution',
      code: 'startup.failed',
      message: 'Startup resolution failed',
      severity: 'error' as const,
    }
    const { client, push } = fakeClient()
    useProvisionalInitialSnapshot(client)
    const { controller, wrapper } = mountWorkspace(client)
    push(frame(1, 7, 'provisional', [pendingOwned], { harnesses: ['devin'] }))
    await tick()
    expect(wrapper.find(`[role="option"][id="row-${pendingOwned.id}"]`).exists()).toBe(true)

    push(frame(2, 7, 'final', [], { drift: 'failed', diagnostics: [failure], pendingChanges: 0 }))
    await tick()
    expect(controller.state.stage).toBe('final')
    expect(controller.state.drift).toBe('failed')
    expect(controller.state.rows).toEqual([])
    expect(controller.state.diagnostics).toEqual([failure])
    expect(wrapper.find(`[role="option"][id="row-${pendingOwned.id}"]`).exists()).toBe(false)
    expect(wrapper.get('ul[aria-label="Diagnostics"]').text()).toContain('Startup resolution failed')
    expect((wrapper.get('button[aria-label="Sync"]').element as HTMLButtonElement).disabled).toBe(true)
    expect((wrapper.get('button[aria-label="Update"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('button[aria-label="Remove Resource"]').exists()).toBe(false)
    expect(client.action).not.toHaveBeenCalled()
    expect(client.configure).not.toHaveBeenCalled()
  })

  // spec: session-startup/ordinary-reload-keeps-the-current-view-until-final-assessment
  it('keeps accepted rows, selection, and detail through ordinary reload stages', async () => {
    const priorWarning = {
      category: 'resolution',
      code: 'assessment.previous',
      message: 'Previous assessment warning',
      severity: 'warning' as const,
    }
    const finalWarning = {
      category: 'resolution',
      code: 'assessment.final',
      message: 'Final assessment warning',
      severity: 'warning' as const,
    }
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(frame(1, 7, 'final', [owned, second], { diagnostics: [priorWarning] }))
    await tick()
    const ownedOption = wrapper.get(`[role="option"][id="row-${owned.id}"]`)
    await ownedOption.trigger('click')
    await tick()
    await wrapper.get(`[role="option"][id="row-${owned.id}"]`).trigger('keydown', { key: 'Enter' })
    await tick()

    const selectedDOMID = selectedOptionID(wrapper)
    const detail = wrapper.get('aside[aria-label="Detail"]')
    const ownedHeading = detail.find('h2').text()
    const ownedDescription = detail.find('p').text()
    const acceptedRows = controller.state.rows.map((candidate) => candidate.id)
    const provisionalDiagnostic = {
      category: 'resolution',
      code: 'stage.provisional',
      message: 'Provisional assessment warning',
      severity: 'warning' as const,
    }
    const configFailure = {
      category: 'config',
      code: 'stage.config.failed',
      message: 'Intermediate configuration failure',
      severity: 'error' as const,
    }
    const inventoryDiagnostic = {
      category: 'catalog',
      code: 'stage.inventory',
      message: 'Intermediate inventory warning',
      severity: 'warning' as const,
    }
    const intermediateFrames: Array<[SessionEvent, Snapshot['diagnostics'][number]]> = [
      [frame(2, 8, 'provisional', [newPending], { drift: 'pending', diagnostics: [provisionalDiagnostic], pendingChanges: 1 }), provisionalDiagnostic],
      [frame(3, 8, 'config', [newPending], { drift: 'failed', diagnostics: [configFailure], pendingChanges: 1 }), configFailure],
      [frame(4, 8, 'inventory', [newVerified], { drift: 'pending', diagnostics: [inventoryDiagnostic], pendingChanges: 1 }), inventoryDiagnostic],
    ]

    for (const [stageFrame, stageDiagnostic] of intermediateFrames) {
      push(stageFrame)
      await tick()
      expect(controller.state.rows.map((candidate) => candidate.id)).toEqual(acceptedRows)
      expect(controller.state.drift).toBe('ready')
      expect(controller.state.diagnostics).toEqual([priorWarning])
      expect(selectedOptionID(wrapper)).toBe(selectedDOMID)
      expect(detail.find('h2').text()).toBe(ownedHeading)
      expect(detail.find('p').text()).toBe(ownedDescription)
      expect(detail.text()).toContain('Drift: ready')
      expect(wrapper.get('ul[aria-label="Diagnostics"]').text()).toContain('Previous assessment warning')
      expect(wrapper.get('ul[aria-label="Diagnostics"]').text()).not.toContain(stageDiagnostic.message)
      expect(wrapper.find(`[role="option"][id="row-${newPending.id}"]`).exists()).toBe(false)
      expect(wrapper.find(`[role="option"][id="row-${newVerified.id}"]`).exists()).toBe(false)
    }

    push(frame(5, 8, 'final', [newVerified], { diagnostics: [finalWarning], pendingChanges: 0 }))
    await tick()
    expect(controller.state.rows.map((candidate) => candidate.id)).toEqual([newVerified.id])
    expect(controller.state.drift).toBe('ready')
    expect(controller.state.diagnostics).toEqual([finalWarning])
    expect(wrapper.find(`[role="option"][id="row-${owned.id}"]`).exists()).toBe(false)
    expect(wrapper.get('ul[aria-label="Diagnostics"]').text()).toContain('Final assessment warning')
    expect(wrapper.get('aside[aria-label="Detail"]').find('.gui-inspector__description').text()).toBe('Verified replacement detail')
  })
})
