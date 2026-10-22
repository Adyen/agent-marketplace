import assert from 'node:assert/strict'
import test from 'node:test'

import { analyticsHead, CLOUDFLARE_BEACON_SRC } from '../src/shared/analytics.ts'

const token = '0123456789abcdef0123456789abcdef'

// spec: site-deployment-profiles/published-public-site-loads-cloudflare-web-analytics
test('public build with a token loads the Cloudflare beacon', () => {
  assert.deepEqual(analyticsHead('public', token), [
    ['script', { defer: '', src: CLOUDFLARE_BEACON_SRC, 'data-cf-beacon': `{"token":"${token}"}` }],
  ])
})

// spec: site-deployment-profiles/unpublished-and-self-hosted-builds-load-no-analytics
test('documentation-only and tokenless builds load no analytics', () => {
  assert.deepEqual(analyticsHead('docs', token), [])
  assert.deepEqual(analyticsHead('public', undefined), [])
  assert.deepEqual(analyticsHead('public', ''), [])
})

test('invalid Cloudflare tokens fail the build', () => {
  for (const invalid of ['ABC', '0123456789ABCDEF0123456789ABCDEF', '0123456789abcdef0123456789abcde"']) {
    assert.throws(() => analyticsHead('public', invalid), /32-character lowercase hex/)
  }
})
