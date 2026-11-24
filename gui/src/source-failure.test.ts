import { describe, expect, it } from 'vitest'
import { describeSourceFailure } from './source-failure'

describe('describeSourceFailure', () => {
  it('summarises known failures in plain words and keeps the raw text as detail', () => {
    const conflict = 'dependency transaction: generation conflict: config read changed at /Users/me/.agents/config.yaml'
    expect(describeSourceFailure(conflict)).toEqual({
      summary: 'Your configuration changed while the source was being added. Try again.',
      detail: conflict,
    })
    expect(describeSourceFailure('fatal: could not resolve host: example.invalid').summary).toContain("Couldn't reach the repository")
    expect(describeSourceFailure("fatal: couldn't find remote ref nope").summary).toContain("wasn't found in the repository")
    expect(describeSourceFailure('something unexpected').summary).toBe("The source couldn't be added.")
  })
})
