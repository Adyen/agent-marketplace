---
# Not content: this file *is* the "/" route, and `layout: LandingPage` is what
# selects the page component registered in .vitepress/theme/index.ts.
# VitePress derives every route from a markdown file, so without this file the
# build emits 404.html and no index.html at all.
#
# The body stays empty — the page itself lives in the theme.
layout: LandingPage
title: A package manager for AI agent tooling
description: Distribute skills, commands, subagents and MCP servers from git registries into the native layout of every AI coding harness you use.
---
