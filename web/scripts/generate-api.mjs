import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import openapiTS, { astToString, COMMENT_HEADER } from 'openapi-typescript'
import { generateClient } from '../../api/generate-client.mjs'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

await generateClient({
  inputPath: resolve(webRoot, '..', 'api', 'openapi.yaml'),
  outputPath: resolve(webRoot, 'app', 'generated', 'api.d.ts'),
  check: process.argv.includes('--check'),
  openapiTS,
  astToString,
  header: COMMENT_HEADER,
})
