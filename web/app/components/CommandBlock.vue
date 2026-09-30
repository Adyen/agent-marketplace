<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import CopyButton from '~/components/CopyButton.vue'
import { useCopyText } from '~/composables/useCopyText'

type Props = {
  /** The command as the user would run it. Empty means there is nothing to show. */
  command: string
  /**
   * What the command is, in object form: the labels are composed as
   * "Copy <label>" and "<label> copied" so a caller cannot describe the same
   * control two inconsistent ways.
   */
  label: string
  /** Withholds the command text and the copy action without hiding the block. */
  disabled?: boolean
}

const props = withDefaults(defineProps<Props>(), { disabled: false })
const { copied, copyError, copy } = useCopyText()

// A command that is present but withheld is still not copyable, so
// availability is one idea rather than two coinciding conditions.
const available = computed(() => Boolean(props.command) && !props.disabled)
const isCopied = computed(() => copied.value === 'command')
const scrollViewport = ref<HTMLElement | null>(null)
const scrollable = ref(false)
const scrolling = ref(false)
const scrollThumbWidth = ref(0)
const scrollThumbOffset = ref(0)
let scrollResizeObserver: ResizeObserver | undefined
let scrollTimer: ReturnType<typeof setTimeout> | undefined

function updateScrollIndicator() {
  const element = scrollViewport.value
  if (!element) return
  const maxScroll = element.scrollWidth - element.clientWidth
  const trackWidth = Math.max(element.clientWidth - 16, 0)
  scrollable.value = maxScroll > 1
  scrollThumbWidth.value = scrollable.value ? Math.max(24, trackWidth * element.clientWidth / element.scrollWidth) : 0
  scrollThumbOffset.value = maxScroll > 0 ? (trackWidth - scrollThumbWidth.value) * element.scrollLeft / maxScroll : 0
}

function handleScroll() {
  updateScrollIndicator()
  scrolling.value = scrollable.value
  if (scrollTimer) clearTimeout(scrollTimer)
  scrollTimer = setTimeout(() => { scrolling.value = false }, 700)
}

onMounted(async () => {
  await nextTick()
  updateScrollIndicator()
  if (typeof ResizeObserver !== 'undefined' && scrollViewport.value) {
    scrollResizeObserver = new ResizeObserver(updateScrollIndicator)
    scrollResizeObserver.observe(scrollViewport.value)
  }
})

watch(() => props.command, async () => {
  await nextTick()
  updateScrollIndicator()
})

onBeforeUnmount(() => {
  scrollResizeObserver?.disconnect()
  if (scrollTimer) clearTimeout(scrollTimer)
})
</script>

<template>
  <div>
    <!-- Filled, unbordered, prompt-prefixed: a command is output to be taken,
         not a field to be typed in, so it must not borrow input chrome. -->
    <div class="grid h-12 grid-cols-[minmax(0,1fr)_auto] overflow-hidden rounded border border-[var(--am-color-code-border)] bg-[var(--am-color-code-bg)]">
      <div class="relative h-full min-w-0 overflow-hidden">
        <pre ref="scrollViewport" data-testid="command-scroll" class="command-scroll m-0 flex h-full min-w-0 items-center gap-3 overflow-x-auto px-4 py-2 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--am-color-action-focus-ring)]" tabindex="0" aria-label="Install command" @scroll.passive="handleScroll"><span class="select-none font-mono text-sm leading-6 text-dimmed" aria-hidden="true">$</span><code
            v-if="available"
            data-testid="command-text"
            class="whitespace-pre font-mono text-sm leading-6 text-[var(--am-color-code-text)]"
          >{{ command }}</code><span v-else class="font-mono text-sm leading-6 text-dimmed">command unavailable</span></pre>
        <div v-if="scrollable" class="pointer-events-none absolute inset-x-2 bottom-1 h-1 overflow-hidden rounded-full transition-opacity duration-150 motion-reduce:transition-none" :class="scrolling ? 'opacity-70' : 'opacity-0'" aria-hidden="true">
          <span class="absolute inset-y-0 left-0 rounded-full bg-[var(--am-color-border-strong)]" :style="{ width: `${scrollThumbWidth}px`, transform: `translateX(${scrollThumbOffset}px)` }" />
        </div>
      </div>

      <div data-testid="command-copy-area" class="flex items-center border-l border-[var(--am-color-code-border)] bg-[var(--am-color-bg-subtle)] p-2">
        <CopyButton
          class="border-primary bg-primary px-3 text-inverted hover:border-primary hover:bg-primary"
          :copied="isCopied"
          :copy-label="`Copy ${label}`"
          :copied-label="`${label} copied`"
          :disabled="!available"
          @click="copy(command, 'command')"
        />
      </div>
    </div>

    <p v-if="copyError === 'command'" role="alert" class="mt-2 text-xs text-error">Clipboard access failed. Select the command and copy it manually.</p>
    <p class="sr-only" aria-live="polite">{{ isCopied ? `${label} copied.` : '' }}</p>
  </div>
</template>

<style scoped>
.command-scroll {
  scrollbar-width: none;
}

.command-scroll::-webkit-scrollbar {
  width: 0;
  height: 0;
}
</style>
