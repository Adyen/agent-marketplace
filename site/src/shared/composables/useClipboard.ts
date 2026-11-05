import { computed, onUnmounted, ref } from 'vue'

export type ClipboardState = 'idle' | 'copied' | 'failed'

const COPIED_HOLD_MS = 1500

export function useClipboard(copiedAnnouncement: string) {
  const state = ref<ClipboardState>('idle')
  let reset: ReturnType<typeof setTimeout> | undefined
  let unmounted = false

  const announcement = computed(
    () => ({ idle: '', copied: copiedAnnouncement, failed: 'Copy failed' })[state.value],
  )

  async function writeText(text: string | (() => Promise<string>)): Promise<void> {
    let next: ClipboardState = 'copied'
    try {
      const value = typeof text === 'function' ? await text() : text
      await navigator.clipboard.writeText(value)
    } catch {
      next = 'failed'
    }
    if (unmounted) return
    clearTimeout(reset)
    state.value = next
    reset = setTimeout(() => (state.value = 'idle'), COPIED_HOLD_MS)
  }

  onUnmounted(() => {
    unmounted = true
    clearTimeout(reset)
  })

  return { announcement, state, writeText }
}
