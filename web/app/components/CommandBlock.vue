<script setup lang="ts">
import { computed } from "vue";
import CopyButton from "~/components/CopyButton.vue";
import { useCopyText } from "~/composables/useCopyText";

type Props = {
  /** The command as the user would run it. Empty means there is nothing to show. */
  command: string;
  /**
   * What the command is, in object form: the labels are composed as
   * "Copy <label>" and "<label> copied" so a caller cannot describe the same
   * control two inconsistent ways.
   */
  label: string;
  /** Withholds the command text and the copy action without hiding the block. */
  disabled?: boolean;
};

const props = withDefaults(defineProps<Props>(), { disabled: false });
const { copied, copyError, copy } = useCopyText();

// A command that is present but withheld is still not copyable, so
// availability is one idea rather than two coinciding conditions.
const available = computed(() => Boolean(props.command) && !props.disabled);
const isCopied = computed(() => copied.value === "command");
</script>

<template>
  <div>
    <!-- Filled, unbordered, prompt-prefixed: a command is output to be taken,
         not a field to be typed in, so it must not borrow input chrome. -->
    <div
      class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 rounded border border-[var(--am-color-code-border)] bg-[var(--am-color-code-bg)] p-2"
    >
      <div class="flex min-w-0 items-center gap-3 overflow-x-auto px-2 py-1">
        <span
          class="select-none font-mono text-sm leading-6 text-dimmed"
          aria-hidden="true"
          >$</span
        >
        <code
          v-if="available"
          data-testid="command-text"
          class="whitespace-pre font-mono text-sm leading-6 text-[var(--am-color-code-text)]"
          >{{ command }}</code
        >
        <span v-else class="font-mono text-sm leading-6 text-dimmed"
          >command unavailable</span
        >
      </div>

      <CopyButton
        class="self-stretch border-primary bg-primary px-3 text-inverted hover:border-primary hover:bg-primary"
        :copied="isCopied"
        :copy-label="`Copy ${label}`"
        :copied-label="`${label} copied`"
        :disabled="!available"
        @click="copy(command, 'command')"
      />
    </div>

    <p
      v-if="copyError === 'command'"
      role="alert"
      class="mt-2 text-xs text-error"
    >
      Clipboard access failed. Select the command and copy it manually.
    </p>
    <p class="sr-only" aria-live="polite">
      {{ isCopied ? `${label} copied.` : "" }}
    </p>
  </div>
</template>
