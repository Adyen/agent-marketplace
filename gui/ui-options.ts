export const guiUiOptions = {
  colorMode: false,
  ui: {
    colors: { primary: 'neutral' },
    checkbox: {
      slots: {
        base: 'rounded-[4px] ring ring-inset ring-accented overflow-hidden focus-visible:outline-3',
        indicator: 'flex items-center justify-center size-full text-[var(--gui-solid-ink)]',
      },
    },
  },
}
