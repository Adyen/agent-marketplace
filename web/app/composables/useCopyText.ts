import { ref } from "vue";

export function useCopyText() {
  const copied = ref<string | null>(null);
  const copyError = ref<string | null>(null);

  async function copy(value: string, target: string) {
    copyError.value = null;
    if (!import.meta.client || !navigator.clipboard || !value) {
      copyError.value = target;
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      copyError.value = target;
      return;
    }
    copied.value = target;
    window.setTimeout(() => {
      if (copied.value === target) copied.value = null;
    }, 1800);
  }

  return { copied, copyError, copy };
}
