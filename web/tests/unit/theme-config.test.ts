import { describe, expect, it, vi } from "vitest";

describe("Marketplace Nuxt UI theme", () => {
  it("uses explicit primary action foreground and interaction states", async () => {
    vi.stubGlobal("defineAppConfig", <T>(config: T) => config);
    const { default: appConfig } = await import("../../app/app.config");

    const primarySolid = appConfig.ui.button.compoundVariants.find(
      (variant) => variant.color === "primary" && variant.variant === "solid",
    );

    expect(primarySolid?.class).toContain(
      "text-[var(--am-color-text-on-action)]",
    );
    expect(primarySolid?.class).toContain(
      "hover:bg-[var(--am-color-action-hover)]",
    );
    expect(primarySolid?.class).toContain(
      "active:bg-[var(--am-color-action-active)]",
    );
  });
});
