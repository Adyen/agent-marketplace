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
    <div class="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-3 gap-y-2 rounded-xl border border-[var(--am-color-code-border)] bg-[var(--am-color-code-bg)] px-4 py-3 sm:grid-cols-[auto_minmax(0,1fr)_auto]">
      <span class="select-none font-mono text-sm leading-6 text-dimmed" aria-hidden="true">$</span>
      <code
        v-if="available"
        data-testid="command-text"
        class="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-sm leading-6 text-[var(--am-color-code-text)]"
      >{{ command }}</code>
      <span v-else class="min-w-0 flex-1 font-mono text-sm leading-6 text-dimmed">command unavailable</span>

      <CopyButton
        class="col-start-2 justify-self-start sm:col-start-3 sm:row-start-1 sm:justify-self-end"
        :copied="isCopied"
        :copy-label="`Copy ${label}`"
        :copied-label="`${label} copied`"
        :disabled="!available"
        @click="copy(command, 'command')"
      />
    </div>

    <p v-if="copyError === 'command'" role="alert" class="mt-2 text-xs text-error">Clipboard access failed. Select the command and copy it manually.</p>
    <p class="sr-only" aria-live="polite">{{ isCopied ? `${label} copied.` : '' }}</p>
  </div>
</template>
