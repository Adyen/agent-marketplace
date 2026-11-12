import { describe, expect, it } from 'vitest'
import { mkdtemp, cp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import openapiTS, { astToString, COMMENT_HEADER } from 'openapi-typescript'
import { generateClient } from '../../api/generate-client.mjs'

const guiRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const contractPath = resolve(guiRoot, '..', 'api', 'gui-local.yaml')
const committedPath = resolve(guiRoot, 'src', 'generated', 'api.d.ts')

// spec: gui-local-api/generated-client-matches-the-published-contract
describe('generated client drift', () => {
  it('reports no drift for the committed generated client', async () => {
    const previous = process.exitCode
    process.exitCode = 0
    await generateClient({
      inputPath: contractPath,
      outputPath: committedPath,
      check: true,
      openapiTS,
      astToString,
      header: COMMENT_HEADER,
    })
    expect(process.exitCode ?? 0).toBe(0)
    process.exitCode = previous
  })

  it('fails when the generated output diverges from a changed contract', async () => {
    const temp = await mkdtemp(join(tmpdir(), 'gui-drift-'))
    try {
      const contract = join(temp, 'gui-local.yaml')
      const output = join(temp, 'api.d.ts')
      await cp(contractPath, contract)
      await generateClient({
        inputPath: contract,
        outputPath: output,
        check: false,
        openapiTS,
        astToString,
        header: COMMENT_HEADER,
      })
      const original = await readFile(contract, 'utf8')
      const mutated = original.replace('progressSequence:', 'progressSequenceV2:')
      if (mutated === original) {
        throw new Error('contract mutation did not apply')
      }
      await writeFile(join(temp, 'gui-local-mutated.yaml'), mutated)
      const previous = process.exitCode
      process.exitCode = 0
      await generateClient({
        inputPath: join(temp, 'gui-local-mutated.yaml'),
        outputPath: output,
        check: true,
        openapiTS,
        astToString,
        header: COMMENT_HEADER,
      })
      expect(process.exitCode).toBe(1)
      process.exitCode = previous
    } finally {
      await rm(temp, { recursive: true, force: true })
    }
  })
})
