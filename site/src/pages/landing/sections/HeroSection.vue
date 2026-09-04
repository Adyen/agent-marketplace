<script setup lang="ts">
import {
  GITHUB_REPOSITORY_URL,
  INSTALL_COMMAND,
} from '../../../shared/site'
import CommandBar from '../components/CommandBar.vue'
import TuiPreview from '../components/TuiPreview.vue'
</script>

<template>
  <header class="hero">
    <div class="am-wrap">
      <div class="grid">
        <div class="copy">
          <h1>
            Manage AI agent tooling once
            <span class="h1-sub">
              Sync it everywhere you work
            </span>
          </h1>
          <p class="lede">
              Manage skills, commands, hooks, and MCP servers from one config, then
              sync them to Claude, Cursor, Codex, and more.
          </p>

          <CommandBar :command="INSTALL_COMMAND" class="install" />

          <div class="actions">
            <a
              class="action secondary"
              :href="GITHUB_REPOSITORY_URL"
              target="_blank"
              rel="noreferrer"
            >
              View on GitHub
              <span aria-hidden="true">↗</span>
            </a>
          </div>
        </div>

        <div class="product-stage">
        <TuiPreview />
          <p class="preview-caption">
            Select resources, review their targets, then sync.
          </p>
        </div>
      </div>

    </div>
  </header>
</template>

<style scoped>
.hero {
  --hero-accent: #55db89;
  --hero-accent-strong: #78eba2;
  position: relative;
  z-index: 1;
  overflow: hidden;
  padding: 120px 0 80px;
  background:
    radial-gradient(circle at 76% 44%, rgba(37, 194, 102, 0.18), transparent 31rem),
    linear-gradient(rgba(85, 219, 137, 0.045) 1px, transparent 1px),
    linear-gradient(90deg, rgba(85, 219, 137, 0.045) 1px, transparent 1px),
    #09110d;
  background-size: auto, 44px 44px, 44px 44px, auto;
  color: #f5f8f6;
}

.hero::after {
  position: absolute;
  right: -14rem;
  bottom: -22rem;
  width: 44rem;
  height: 44rem;
  border: 1px solid rgba(85, 219, 137, 0.12);
  border-radius: 50%;
  background: radial-gradient(circle, rgba(85, 219, 137, 0.1), transparent 65%);
  content: "";
  pointer-events: none;
}

.grid {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(480px, 1fr);
  gap: clamp(52px, 6vw, 88px);
  align-items: start;
}

.copy {
  min-width: 0;
}

h1 {
  margin: 0 0 20px;
  color: #f7faf8;
  font-size: clamp(56px, 5.2vw, 72px);
  line-height: 0.99;
  letter-spacing: -0.052em;
  font-weight: 750;
}

.h1-sub {
  display: block;
  margin-top: 20px;
  color: #b9c8bf;
  font-size: clamp(23px, 2.1vw, 29px);
  font-weight: 520;
  line-height: 1.32;
  letter-spacing: -0.025em;
}

.lede {
  max-width: 56ch;
  margin: 0;
  color: #aebcb4;
  font-size: 18px;
  line-height: 1.66;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 14px;
}

.action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-height: 46px;
  padding: 0 19px;
  border: 1px solid transparent;
  border-radius: var(--am-radius-lg);
  font-size: 14px;
  font-weight: 650;
  line-height: 1;
  text-decoration: none;
  transition:
    border-color 0.16s ease,
    background-color 0.16s ease,
    color 0.16s ease,
    transform 0.16s ease;
}

.action:hover {
  transform: translateY(-1px);
}

.action:focus-visible {
  outline: 2px solid var(--hero-accent);
  outline-offset: 3px;
}

.secondary {
  border-color: rgba(255, 255, 255, 0.17);
  background: rgba(255, 255, 255, 0.055);
  color: #e3eae6;
}

.secondary:hover {
  border-color: rgba(85, 219, 137, 0.45);
  background: rgba(85, 219, 137, 0.08);
  color: #ffffff;
}

.install {
  --am-code-bg: rgba(4, 9, 6, 0.78);
  --am-code-border: rgba(85, 219, 137, 0.2);
  --am-code-shadow: rgba(0, 0, 0, 0.75);
  --am-code-text: #eef5f0;
  --am-on-accent: #07130c;
  --vp-c-brand-1: var(--hero-accent);
  --vp-c-brand-2: var(--hero-accent-strong);
  --am-technical-font-size: 14px;
  --am-technical-padding: 15px;
  margin-top: 30px;
  padding-block: 9px;
  box-shadow:
    0 20px 50px -30px rgba(0, 0, 0, 0.9),
    inset 0 1px rgba(255, 255, 255, 0.03);
}

.install :deep(.copy) {
  padding: 8px 12px;
  font-size: 12px;
}

.product-stage {
  --am-code-bg: #0b0f0d;
  --am-code-bg-raised: #121915;
  --am-code-border: rgba(167, 204, 180, 0.17);
  --am-code-text: #edf4ef;
  --am-code-muted: #9eb0a5;
  --am-code-cyan: #7cd7da;
  --am-code-warning: #f3c36d;
  --am-code-selection: rgba(85, 219, 137, 0.12);
  --vp-c-brand-1: var(--hero-accent);
  position: relative;
  min-width: 0;
  padding: 0 14px 14px;
}

.product-stage :deep(.terminal) {
  box-shadow:
    0 34px 44px rgba(0, 0, 0, 0.34),
    0 0 52px rgba(45, 190, 104, 0.13);
}

.preview-caption {
  margin: 12px 20px 2px;
  color: #91a198;
  font-size: 13.5px;
  line-height: 1.5;
}

@media (max-width: 1020px) {
  .hero {
    padding-top: 104px;
  }

  .grid {
    grid-template-columns: 1fr;
    gap: 42px;
  }

  .copy {
    max-width: 720px;
  }

}

@media (max-width: 680px) {
  .hero {
    padding: 88px 0 64px;
    background-size: auto, 30px 30px, 30px 30px, auto;
  }

  h1 {
    font-size: 43px;
    letter-spacing: -0.045em;
  }

  .h1-sub {
    margin-top: 15px;
    font-size: 22px;
  }

  .lede {
    font-size: 16px;
  }

  .action {
    flex: 1;
  }

  .install {
    --am-technical-font-size: 12px;
    padding-block: 7px;
  }

}

@media (prefers-reduced-motion: reduce) {
  .action {
    transition: none;
  }

  .action:hover {
    transform: none;
  }
}
</style>
