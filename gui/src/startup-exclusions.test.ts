// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import type { SessionEvent, Snapshot } from './api/client'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

function frame(sequence: number, generation: number, stage: Snapshot['stage'], rows: ReturnType<typeof row>[]): SessionEvent {
  return event({ sequence, generation, operation: null, payload: snapshot(rows, { stage, drift: 'pending', harnesses: ['devin'] }) })
}

function mountedWorkspace() {
  const fake = fakeClient()
  vi.mocked(fake.client.snapshot).mockResolvedValue(snapshot([], { stage: 'provisional', drift: 'pending', harnesses: ['devin'] }))
  return { ...fake, ...mountWorkspace(fake.client) }
}

async function pushInitialStages(push: ReturnType<typeof fakeClient>['push'], generation: number, rows: ReturnType<typeof row>[]): Promise<void> {
  push(frame(1, generation, 'provisional', rows))
  await tick()
  push(frame(2, generation, 'config', rows))
  await tick()
}

function expectNoMutations(client: ReturnType<typeof fakeClient>['client']): void {
  expect(client.action).not.toHaveBeenCalled()
  expect(client.configure).not.toHaveBeenCalled()
  expect(client.resourcePlan).not.toHaveBeenCalled()
  expect(client.resourceAction).not.toHaveBeenCalled()
}

function directoryRows(scope: 'user' | 'available', checked: boolean, pending: boolean) {
  return ['alpha', 'beta'].map((name) => row(name, {
    id: `directory-${name}`, registry: 'local', scope, checked, group: checked ? 'enabled' : 'available',
    status: pending ? 'enabled in config; verification pending' : 'available', pendingVerification: pending, resourceAction: 'none',
  }))
}

function organizationRows(required: boolean, excludeReview: boolean, pending: boolean) {
  const reviewExcluded = excludeReview && !required
  const auditStatus = pending ? 'enabled in config; verification pending' : 'enabled in config'
  const reviewStatus = reviewExcluded ? (pending ? 'excluded; verification pending' : 'excluded') : auditStatus
  const group = required ? 'required' : 'enabled'
  const optionalAction = pending || required ? 'none' : 'exclude'
  const reviewAction = pending || required ? 'none' : reviewExcluded ? 'restore' : 'exclude'
  return [
    row('audit', { id: 'organization-audit', registry: 'mp', scope: 'organization', required, checked: true, group, status: auditStatus, pendingVerification: pending, resourceAction: optionalAction }),
    row('review', { id: 'organization-review', registry: 'mp', scope: 'organization', required, checked: required || !excludeReview, group: required ? 'required' : reviewExcluded ? 'available' : 'enabled', status: reviewStatus, pendingVerification: pending, resourceAction: reviewAction }),
  ]
}

describe('startup exclusion consumers', () => {
  it('renders the selected Directory control rows before verification', async () => {
    const { client, push, controller, wrapper } = mountedWorkspace()
    const rows = directoryRows('user', true, true)
    await pushInitialStages(push, 7, rows)
    expect(controller.state.rows.map((item) => item.name)).toEqual(['alpha', 'beta'])
    expect(controller.state.rows.every((item) => item.scope === 'user' && item.checked && item.pendingVerification)).toBe(true)
    const options = wrapper.findAll('[role="option"]')
    expect(options).toHaveLength(2)
    for (const option of options) {
      expect(option.find('.workspace__check').classes()).toContain('workspace__check--on')
      expect(option.find('.workspace__scope').text()).toBe('user')
    }
    expectNoMutations(client)
  })

  // spec: session-startup/excluded-only-configuration-publishes-no-provisional-rows
  it('keeps excluded-only rows empty until the available inventory stage', async () => {
    const { client, push, controller, wrapper } = mountedWorkspace()
    await pushInitialStages(push, 8, [])
    expect(controller.state.stage).toBe('config')
    expect(controller.state.rows).toEqual([])
    expect(wrapper.findAll('[role="option"]')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('alpha')
    expect(wrapper.text()).not.toContain('beta')
    expect(controller.state.drift).toBe('pending')
    expectNoMutations(client)

    push(frame(3, 8, 'inventory', directoryRows('available', false, false)))
    await tick()
    expect(controller.state.stage).toBe('inventory')
    expect(controller.state.drift).toBe('pending')
    expect(controller.state.rows.every((item) => item.scope === 'available' && !item.checked && !item.pendingVerification)).toBe(true)
    for (const option of wrapper.findAll('[role="option"]')) {
      expect(option.find('.workspace__check').classes()).not.toContain('workspace__check--on')
      expect(option.find('.workspace__scope').text()).toBe('available')
    }
    expectNoMutations(client)
  })

  // spec: session-startup/excluded-configured-selections-render-unchecked-in-their-verified-position
  it('keeps an optional organization exclusion unchecked through verified inventory', async () => {
    const { client, push, controller, wrapper } = mountedWorkspace()
    const pendingRows = organizationRows(false, true, true)
    await pushInitialStages(push, 9, pendingRows)
    expect(controller.state.rows.map((item) => item.id)).toEqual(pendingRows.map((item) => item.id))
    const audit = wrapper.get('[role="option"][id="row-organization-audit"]')
    const review = wrapper.get('[role="option"][id="row-organization-review"]')
    expect(wrapper.findAll('[role="option"]').map((option) => option.attributes('id'))).toEqual([
      'row-organization-audit', 'row-organization-review',
    ])
    expect(audit.find('.workspace__check').classes()).toContain('workspace__check--on')
    expect(review.find('.workspace__check').classes()).not.toContain('workspace__check--on')
    expect(review.find('.workspace__scope').text()).toBe('organization')
    expect(controller.state.rows[1]?.checked).toBe(false)
    expect(controller.state.rows[1]?.pendingVerification).toBe(true)
    expect(controller.state.rows[1]?.group).toBe('available')
    expect((wrapper.get('button[aria-label="Sync"]').element as HTMLButtonElement).disabled).toBe(true)
    expect((wrapper.get('button[aria-label="Update"]').element as HTMLButtonElement).disabled).toBe(true)

    await review.trigger('click')
    await tick()
    expect(review.attributes('aria-selected')).toBe('true')
    expect(review.find('.workspace__check').classes()).not.toContain('workspace__check--on')
    expect(controller.state.rows[1]?.checked).toBe(false)
    expect(wrapper.get('aside[aria-label="Detail"]').text()).toContain('excluded; verification pending')
    expect(wrapper.find('button[aria-label="Install Resource"]').exists()).toBe(false)
    expect(wrapper.find('button[aria-label="Remove Resource"]').exists()).toBe(false)
    await review.trigger('keydown', { key: 'Enter' })
    await tick()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expectNoMutations(client)

    const verifiedRows = organizationRows(false, true, false)
    push(frame(3, 9, 'inventory', verifiedRows))
    await tick()
    expect(controller.state.rows.map((item) => item.id)).toEqual(verifiedRows.map((item) => item.id))
    expect(controller.state.rows.map((item) => item.checked)).toEqual([true, false])
    expect(controller.state.rows.map((item) => item.scope)).toEqual(['organization', 'organization'])
    expect(controller.state.rows.map((item) => item.harnesses.join(','))).toEqual(['devin', 'devin'])
    expect(controller.state.rows.map((item) => item.group)).toEqual(['enabled', 'available'])
    expect(controller.state.rows[1]?.status).toBe('excluded')
    expect(controller.state.rows[1]?.pendingVerification).toBe(false)
    expect(wrapper.get('[role="option"][id="row-organization-review"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('button[aria-label="Restore Resource"]').exists()).toBe(true)
    expect(wrapper.get('[role="option"][id="row-organization-audit"]').find('.workspace__check').classes()).toContain('workspace__check--on')
    expect(wrapper.get('[role="option"][id="row-organization-review"]').find('.workspace__check').classes()).not.toContain('workspace__check--on')
    expectNoMutations(client)
  })

  // spec: session-startup/excluded-configured-selections-render-unchecked-in-their-verified-position
  it('keeps required organization rows checked under the user-personal exclusion', async () => {
    const { client, push, controller, wrapper } = mountedWorkspace()
    const rows = organizationRows(true, true, true)
    await pushInitialStages(push, 10, rows)
    expect(controller.state.rows.every((item) => item.required && item.checked && item.group === 'required')).toBe(true)
    const review = wrapper.get('[role="option"][id="row-organization-review"]')
    expect(review.find('.workspace__check').classes()).toContain('workspace__check--on')
    expect(review.find('.workspace__scope').text()).toBe('organization')
    await review.trigger('click')
    await tick()
    expect(wrapper.find('button[aria-label="Install Resource"]').exists()).toBe(false)
    expect(wrapper.find('button[aria-label="Remove Resource"]').exists()).toBe(false)
    expectNoMutations(client)
  })
})
