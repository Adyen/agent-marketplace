<script setup lang="ts">
import { computed } from 'vue'
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
const actionLabel = computed(() => (isCopied.value ? `${props.label} copied` : `Copy ${props.label}`))
</script>

<template>
  <div>
    <!-- Filled, unbordered, prompt-prefixed: a command is output to be taken,
         not a field to be typed in, so it must not borrow input chrome. -->
    <div class="flex items-start gap-3 rounded-xl bg-muted px-4 py-3">
      <span class="select-none font-mono text-sm leading-6 text-dimmed" aria-hidden="true">$</span>
      <code
        v-if="available"
        data-testid="command-text"
        class="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-sm leading-6 text-highlighted"
      >{{ command }}</code>
      <span v-else class="min-w-0 flex-1 font-mono text-sm leading-6 text-dimmed">command unavailable</span>

      <!-- The block is bg-muted, so the control gets its own resting surface
           and a stronger border and fill on hover.
           Saturation is reserved for install verdicts, so a successful copy is
           confirmed with a check and full ink, never a colour. -->
      <UButton
        type="button"
        size="xs"
        color="neutral"
        variant="ghost"
        :icon="isCopied ? 'i-lucide-check' : 'i-lucide-copy'"
        :class="[
          'shrink-0 cursor-pointer rounded border bg-default font-mono text-[11px] uppercase tracking-[0.12em] transition-colors hover:border-accented hover:bg-accented hover:text-highlighted disabled:cursor-not-allowed motion-reduce:transition-none',
          isCopied ? 'border-accented bg-accented text-highlighted' : 'border-default text-muted',
        ]"
        :aria-label="actionLabel"
        :title="actionLabel"
        :disabled="!available"
        @click="copy(command, 'command')"
      >
        {{ isCopied ? 'Copied' : 'Copy' }}
      </UButton>
    </div>

    <p v-if="copyError === 'command'" role="alert" class="mt-2 text-xs text-error">Clipboard access failed. Select the command and copy it manually.</p>
    <p class="sr-only" aria-live="polite">{{ isCopied ? `${label} copied.` : '' }}</p>
  </div>
</template>
