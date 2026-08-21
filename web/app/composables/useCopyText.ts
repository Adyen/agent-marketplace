import { ref } from 'vue'

export function useCopyText() {
  const copied = ref<string | null>(null)

  async function copy(value: string, target: string) {
    if (!import.meta.client || !navigator.clipboard || !value) return
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      return
    }
    copied.value = target
    window.setTimeout(() => {
      if (copied.value === target) copied.value = null
    }, 1800)
  }

  return { copied, copy }
}
