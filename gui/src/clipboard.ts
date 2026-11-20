export async function copyText(text: string): Promise<boolean> {
  const runtime = window.runtime
  if (runtime?.ClipboardSetText != null) {
    return await runtime.ClipboardSetText(text)
  }
  await navigator.clipboard.writeText(text)
  return true
}
