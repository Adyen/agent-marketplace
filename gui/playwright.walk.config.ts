import { defineConfig } from '@playwright/test'
import base from './playwright.config'

export default defineConfig({
  ...base,
  testDir: './tests/walk',
  testMatch: '**/*.spec.ts',
  outputDir: './test-results/walk',
})
