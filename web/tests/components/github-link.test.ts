import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import GitHubLink from "../../app/components/GitHubLink.vue";

describe("GitHubLink", () => {
  it("links to the agent-marketplace GitHub repository in a new tab", () => {
    const link = mount(GitHubLink).get("a");

    expect(link.attributes()).toMatchObject({
      href: "https://github.com/Adyen/agent-marketplace",
      target: "_blank",
      rel: "noopener noreferrer",
      "aria-label": "View agent-marketplace on GitHub",
    });
    expect(link.get("svg").attributes("aria-hidden")).toBe("true");
  });
});
