import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AdminRegistryFields from '../../app/components/AdminRegistryFields.vue'
import { newAdminRegistry } from '../../app/lib/admin'
import { adminStubs } from './admin-stubs'

const profiles = { profiles: [{ id: 'github', allowed_origins: ['https://github.com'] }], source_harnesses: ['claude'] }

function render(errors?: Record<string, string>) {
  const registry = { ...newAdminRegistry(), id: 'mattpocock-skills', slug: 'mattpocock/skills', origin: 'https://github.com/mattpocock/skills.git', roots: [{ path: '.', source_harness: '' }, { path: '../x', source_harness: '' }] }
  return mount(AdminRegistryFields, { props: { modelValue: registry, profiles, existing: false, errors }, global: { stubs: { ...adminStubs, AdminPolicyFields: true } } })
}

describe('admin registry fields', () => {
  it('outlines and describes exactly the fields the API rejected', () => {
    const wrapper = render({ 'slug': '"mattpocock/skills" must use only lowercase letters, digits and hyphens', 'roots[1].path': '"../x" must be relative and not contain \'..\'', 'publisher.name': 'Publisher name is too long' })
    const slug = wrapper.find('#registry-slug')
    expect(slug.attributes('aria-invalid')).toBe('true')
    expect(slug.attributes('aria-describedby')).toBe('registry-slug-help')
    expect(wrapper.find('#registry-slug-help').text()).toContain('lowercase letters, digits and hyphens')
    expect(wrapper.find('#root-1').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('#root-1-help').text()).toContain('must be relative')
    expect(wrapper.find('#root-0').attributes('aria-invalid')).toBeUndefined()
    expect(wrapper.find('#publisher-name').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('#publisher-name').attributes('aria-describedby')).toBe('publisher-name-help')
    expect(wrapper.find('#publisher-name-help').text()).toContain('Publisher name is too long')
    expect(wrapper.find('#registry-origin').attributes('aria-invalid')).toBeUndefined()
    expect(wrapper.find('#registry-origin-help').text()).toContain('Approved HTTPS or SSH origin')
    expect(wrapper.findAll('[data-invalid="true"]')).toHaveLength(3)
  })

  it('renders plain help text when nothing is rejected', () => {
    const wrapper = render()
    expect(wrapper.findAll('[data-invalid="true"]')).toHaveLength(0)
    expect(wrapper.findAll('[aria-invalid="true"]')).toHaveLength(0)
    expect(wrapper.find('#registry-ref-help').text()).toContain('Branch, tag or commit SHA')
  })
})
