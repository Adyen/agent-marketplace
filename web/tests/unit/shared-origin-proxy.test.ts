import { spawnSync } from 'node:child_process'
import { resolve } from 'node:path'
import { expect, it } from 'vitest'

it('runs shared-origin proxy routing and origin-containment regressions with native Node HTTP', () => {
  const result = spawnSync(process.execPath, ['--test', resolve('tests/unit/shared-origin-proxy.node.mjs')], {
    encoding: 'utf8',
    timeout: 10_000,
  })
  expect(result.error).toBeUndefined()
  expect(result.status, result.stdout + result.stderr).toBe(0)
})
