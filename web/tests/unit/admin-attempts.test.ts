import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AdminAttempts from '../../app/components/AdminAttempts.vue'
import { adminStubs } from './admin-stubs'

const event = { sequence: 9, registry_id: 'team', job_id: 'refresh-child', parent_job_id: 'completed-parent', attempt: 1, outcome: 'failed', operation: 'reconcile', recorded_at: '2026-09-19T12:00:00Z', configuration_revision: 2, registry_generation: 3, policy_revision: 1 }
const wrappers: ReturnType<typeof mount>[] = []
function render() {
  const wrapper = mount(AdminAttempts, { props: { registryId: 'team' }, global: { stubs: adminStubs } })
  wrappers.push(wrapper)
  return wrapper
}
function response(attempts: unknown[], next_before = 0) {
  return new Response(JSON.stringify({ attempts, next_before }), { headers: { 'Content-Type': 'application/json' } })
}
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers.length = 0; vi.unstubAllGlobals() })

describe('registry indexing attempts', () => {
  it('shows failed child attempts separately from the parent and loads older events using the server cursor', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(response([event], 9)).mockResolvedValueOnce(response([{ ...event, sequence: 8, outcome: 'started' }]))
    vi.stubGlobal('fetch', fetcher)
    const wrapper = render()
    await flushPromises()
    expect(fetcher.mock.calls[0]?.[0]).toBe('/api/v1/admin/attempts?registry_id=team&limit=25')
    const article = wrapper.find('article')
    expect(article.find('[data-color="error"]').text()).toBe('Failed')
    expect(article.text()).toContain('refresh-child')
    expect(article.text()).toContain('completed-parent')
    expect(article.find('time').attributes('datetime')).toBe('2026-09-19T12:00:00Z')
    expect(article.find('button[aria-label^="Copy Job ID refresh-child"]').exists()).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'Load older attempts')!.trigger('click')
    await flushPromises()
    expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/admin/attempts?registry_id=team&limit=25&before=9')
    expect(wrapper.findAll('article')).toHaveLength(2)
    expect(wrapper.findAll('article')[1]!.text()).toContain('Started')
    expect(wrapper.text()).not.toContain('Load older attempts')
  })

  it('retains loaded events on errors, allows retry, and replaces them on refresh', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(response([event], 9)).mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(response([{ ...event, sequence: 10, outcome: 'succeeded', publication_id: 'published-10' }]))
    vi.stubGlobal('fetch', fetcher)
    const wrapper = render()
    await flushPromises()
    const refresh = () => wrapper.findAll('button').find(button => button.text() === 'Refresh indexing attempts')!.trigger('click')
    await refresh()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('article').text()).toContain('Failed')
    await refresh()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findAll('article')).toHaveLength(1)
    expect(wrapper.find('article').text()).toContain('Succeeded')
    expect(wrapper.find('article').text()).toContain('published-10')
    expect(fetcher.mock.calls[2]?.[0]).not.toContain('before=')
  })

  // spec: marketplace-publication/failed-publication-records-the-conflict-cause
  it.each([
    { outcome: 'failed', failure_code: 'publication_conflict', failure_cause: 'Publication conflicts with an existing resource revision.' },
    { outcome: 'superseded', failure_code: 'superseded', failure_cause: 'A newer configuration superseded this refresh.' },
  ])('shows recorded diagnostics for $outcome attempts', async (diagnostics) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([{ ...event, ...diagnostics }])))
    const wrapper = render()
    await flushPromises()
    const details = wrapper.find('[aria-label="Attempt diagnostics"]')
    expect(details.exists()).toBe(true)
    expect(details.find('code').text()).toBe(diagnostics.failure_code)
    expect(details.text()).toContain(diagnostics.failure_cause)
    expect(wrapper.html()).not.toContain('not returned by this endpoint')
  })

  it.each(['failed', 'superseded'])('explains absent historical diagnostics for %s attempts', async (outcome) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([{ ...event, outcome }])))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[aria-label="Attempt diagnostics"]').text()).toContain('No failure details were recorded for this attempt.')
    expect(wrapper.html()).not.toContain('not returned by this endpoint')
  })

  it.each(['started', 'succeeded'])('does not show failure details for %s attempts', async (outcome) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([{ ...event, outcome, failure_code: 'unexpected', failure_cause: 'Unexpected details' }])))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[aria-label="Attempt diagnostics"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Unexpected details')
  })

  it('renders diagnostic values as text rather than markup', async () => {
    const failure_code = '<img src=x onerror=alert(1)>'
    const failure_cause = '<script>alert(1)</script>'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([{ ...event, failure_code, failure_cause }])))
    const wrapper = render()
    await flushPromises()
    const details = wrapper.find('[aria-label="Attempt diagnostics"]')
    expect(details.text()).toContain(failure_code)
    expect(details.text()).toContain(failure_cause)
    expect(details.find('img').exists()).toBe(false)
    expect(details.find('script').exists()).toBe(false)
  })

  it('does not present a failed initial request as an empty successful history', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('No indexing attempts')
  })
})
