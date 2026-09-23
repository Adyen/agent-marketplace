export default defineAppConfig({
  ui: {
    colors: {
      // Scales are defined by the Nuxt adapter in design-tokens.css. Status
      // colors map to Marketplace outcomes; primary maps to the shared action
      // color and is never used as an availability claim.
      primary: "focus",
      neutral: "ink",
      success: "ready",
      warning: "withheld",
      error: "revoked",
    },
    alert: {
      slots: {
        description: "text-sm opacity-100",
      },
    },
    button: {
      compoundVariants: [
        {
          variant: ["solid", "outline", "soft", "subtle", "ghost", "link"],
          class:
            "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--am-color-action-focus-ring)]",
        },
        {
          color: "primary",
          variant: "solid",
          class:
            "text-[var(--am-color-text-on-action)] hover:bg-[var(--am-color-action-hover)] active:bg-[var(--am-color-action-active)] disabled:bg-[var(--am-color-action-primary)] aria-disabled:bg-[var(--am-color-action-primary)]",
        },
        {
          color: "neutral",
          variant: "outline",
          class:
            "border border-[var(--am-color-border-default)] bg-[var(--am-color-bg-subtle)] text-[var(--am-color-text-primary)] ring-0 hover:bg-[var(--am-color-bg-hover)] active:bg-[var(--am-color-bg-hover)] disabled:bg-[var(--am-color-bg-subtle)] aria-disabled:bg-[var(--am-color-bg-subtle)]",
        },
      ],
    },
    input: {
      compoundVariants: [
        {
          variant: "outline",
          class:
            "border border-[var(--am-color-border-control)] bg-[var(--am-color-bg-control)] ring-0",
        },
        {
          variant: ["outline", "soft", "subtle", "ghost"],
          class:
            "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--am-color-action-focus-ring)]",
        },
      ],
    },
    select: {
      slots: {
        content:
          "border border-[var(--am-color-border-default)] bg-[var(--am-color-bg-raised)] ring-0",
        item: "data-highlighted:not-data-disabled:before:bg-[var(--am-color-bg-hover)] data-[state=checked]:text-primary data-[state=checked]:before:bg-[var(--am-color-action-soft)]",
      },
      compoundVariants: [
        {
          variant: "outline",
          class:
            "border border-[var(--am-color-border-control)] bg-[var(--am-color-bg-control)] ring-0 hover:bg-[var(--am-color-bg-control)]",
        },
        {
          variant: ["outline", "soft", "subtle", "ghost"],
          class:
            "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--am-color-action-focus-ring)]",
        },
        {
          highlight: true,
          class: "bg-[var(--am-color-action-soft)] text-primary",
        },
      ],
    },
  },
});
