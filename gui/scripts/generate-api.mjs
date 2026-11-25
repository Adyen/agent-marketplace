import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import openapiTS, { astToString, COMMENT_HEADER } from 'openapi-typescript'
import { generateClient } from '../../api/generate-client.mjs'

const guiRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

await generateClient({
  inputPath: resolve(guiRoot, '..', 'api', 'gui-local.yaml'),
  outputPath: resolve(guiRoot, 'src', 'generated', 'api.d.ts'),
  check: process.argv.includes('--check'),
  openapiTS,
  astToString,
  header: COMMENT_HEADER,
})
