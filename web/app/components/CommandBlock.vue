<script setup lang="ts">
import { computed } from 'vue'
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
</script>

<template>
  <div>
    <!-- Filled, unbordered, prompt-prefixed: a command is output to be taken,
         not a field to be typed in, so it must not borrow input chrome. -->
    <div data-testid="command-container" class="grid h-12 grid-cols-[minmax(0,1fr)_auto] overflow-hidden rounded border border-[var(--am-color-code-border)] bg-[var(--am-color-code-bg)]">
      <pre data-testid="command-scroll" class="command-scroll m-0 flex h-full min-w-0 items-center gap-3 overflow-x-auto overflow-y-hidden px-4 pt-3 pb-1 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--am-color-action-focus-ring)]" tabindex="0" aria-label="Install command"><span class="select-none font-mono text-sm leading-6 text-dimmed" aria-hidden="true">$</span><code
          v-if="available"
          data-testid="command-text"
          class="whitespace-pre font-mono text-sm leading-6 text-[var(--am-color-code-text)]"
        >{{ command }}</code><span v-else class="font-mono text-sm leading-6 text-dimmed">command unavailable</span></pre>

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
  scrollbar-color: transparent transparent;
  scrollbar-width: thin;
}

.command-scroll:hover,
.command-scroll:focus-visible {
  scrollbar-color: var(--am-color-border-strong) transparent;
}

.command-scroll::-webkit-scrollbar {
  height: 6px;
}

.command-scroll::-webkit-scrollbar-track,
.command-scroll::-webkit-scrollbar-thumb {
  background: transparent;
}

.command-scroll:hover::-webkit-scrollbar-thumb,
.command-scroll:focus-visible::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: var(--am-color-border-strong);
}
</style>
