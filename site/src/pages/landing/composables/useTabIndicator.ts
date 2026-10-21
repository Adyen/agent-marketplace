import { nextTick, onBeforeUnmount, onMounted, ref, watch, type Ref, type WatchSource } from 'vue'

/**
 * Measures the selected tab so one decorative indicator can follow it.
 * The indicator is placed immediately on mount, resize and font load, and
 * only animates after the user changes the selection.
 */
export function useTabIndicator(list: Ref<HTMLElement | null>, selected: WatchSource<unknown>) {
  const placement = ref<{ width: string; transform: string } | null>(null)
  const animated = ref(false)
  let resizeObserver: ResizeObserver | undefined
  let disposed = false

  function place(animate: boolean) {
    const current = list.value?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
    if (!current?.offsetWidth) return

    animated.value = animate
    placement.value = {
      width: `${current.offsetWidth}px`,
      transform: `translate(${current.offsetLeft}px, ${current.offsetTop}px)`,
    }
  }

  onMounted(() => {
    place(false)
    if (typeof ResizeObserver !== 'undefined' && list.value) {
      resizeObserver = new ResizeObserver(() => place(false))
      resizeObserver.observe(list.value)
    }
    document.fonts?.ready?.then(() => !disposed && place(false))
  })

  onBeforeUnmount(() => {
    disposed = true
    resizeObserver?.disconnect()
  })

  watch(selected, () => nextTick(() => place(true)))

  return { placement, animated }
}
