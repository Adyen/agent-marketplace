import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CommandBlock from '../../app/components/CommandBlock.vue'

const stubs = {
  UButton: defineComponent({ setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) } }),
}

function mountBlock(props: { command: string, label: string, disabled?: boolean }) {
  return mount(CommandBlock, { props, global: { stubs } })
}

describe('CommandBlock', () => {
  it('shows the command as copyable terminal output rather than an input', () => {
    const wrapper = mountBlock({ command: 'agent-marketplace install https://market.example', label: 'Marketplace install command' })

    expect(wrapper.get('[data-testid="command-text"]').text()).toBe('agent-marketplace install https://market.example')
    // The prompt glyph is decoration for a screen reader: the command text
    // is the content, and "$" is not part of what gets copied.
    expect(wrapper.get('[aria-hidden="true"]').text()).toBe('$')
    // Input chrome is what made this read as a second search field.
    expect(wrapper.get('div > div').classes()).not.toContain('border')
    expect(wrapper.find('input').exists()).toBe(false)
  })

  it('composes both action labels from one description of the command', () => {
    const wrapper = mountBlock({ command: 'agent-marketplace install foo', label: 'pinned CLI install command for foo' })

    const button = wrapper.get('button')
    expect(button.attributes('aria-label')).toBe('Copy pinned CLI install command for foo')
    expect(button.text()).toBe('Copy command')
    expect(button.attributes('disabled')).toBeUndefined()
  })

  it('withholds the command text and the copy action when the command is withheld', () => {
    const wrapper = mountBlock({ command: 'agent-marketplace install foo', label: 'install command', disabled: true })

    expect(wrapper.find('[data-testid="command-text"]').exists()).toBe(false)
    expect(wrapper.find('code').exists()).toBe(false)
    expect(wrapper.text()).toContain('command unavailable')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })

  it('refuses to offer a copy action for an empty command', () => {
    const wrapper = mountBlock({ command: '', label: 'install command' })

    expect(wrapper.find('code').exists()).toBe(false)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })

  it('reports a clipboard failure in place instead of silently doing nothing', async () => {
    const wrapper = mountBlock({ command: 'agent-marketplace install foo', label: 'install command' })

    // happy-dom exposes no clipboard, which is the same observable situation as
    // a browser denying permission.
    await wrapper.get('button').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[role="alert"]').text()).toContain('Clipboard access failed')
    expect(wrapper.get('button').attributes('aria-label')).toBe('Copy install command')
  })
})
