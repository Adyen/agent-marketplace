## 1. Implementation

- [x] 1.1 Extract the docs sidebar to `docsSidebar` in `site/.vitepress/config.mts`
- [x] 1.2 Add `site/.vitepress/llms.ts` and call it from `buildEnd` to write `llms.txt`, per-page Markdown copies, and copied images
- [x] 1.3 Serve `.md` as `text/markdown; charset=utf-8` in the site Dockerfile nginx config
- [x] 1.4 Note the llms.txt coupling in `site/README.md`

## 2. Tests

- [x] 2.1 Add `llm-index-lists-every-documentation-page` and `markdown-copies-mirror-documentation-sources` tests in `site/tests/public-site.test.ts`
- [x] 2.2 Add `both-profiles-publish-the-same-markdown-copies` test in `site/tests/docs-profile.test.ts`

## 3. Archive

- [x] 3.1 Run `openspec archive publish-llms-txt --yes`
