// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import StatusBar from './StatusBar.vue'
import { tick } from './test-support'

function pressKey(key: string) {
  window.dispatchEvent(new KeyboardEvent('keydown', { key }))
}

describe('status bar keyboard hints', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  // spec: gui-shell/keyboard-hints-appear-after-first-keyboard-navigation
  it('shows the keyboard hints only after the first keyboard navigation and keeps them after', async () => {
    const wrapper = mount(StatusBar, { props: { pendingCount: 0 }, attachTo: document.body })
    expect(wrapper.find('.gui-statusbar__hints').exists()).toBe(false)

    wrapper.find('.gui-statusbar__status').trigger('click')
    await tick()
    expect(wrapper.find('.gui-statusbar__hints').exists()).toBe(false)

    pressKey('a')
    await tick()
    expect(wrapper.find('.gui-statusbar__hints').exists()).toBe(false)

    pressKey('ArrowDown')
    await tick()
    expect(wrapper.find('.gui-statusbar__hints').exists()).toBe(true)

    pressKey('a')
    await tick()
    expect(wrapper.find('.gui-statusbar__hints').exists()).toBe(true)
    wrapper.unmount()
  })
})
