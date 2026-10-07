import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname } from 'node:path'
import { pathToFileURL } from 'node:url'






export async function generateClient({ inputPath, outputPath, check, openapiTS, astToString, header }) {
  const generated = `${header}${astToString(await openapiTS(pathToFileURL(inputPath), {
    alphabetize: true,
  }))}`

  if (!check) {
    await mkdir(dirname(outputPath), { recursive: true })
    await writeFile(outputPath, generated, 'utf8')
    console.log(`Generated ${outputPath}`)
    return
  }

  let committed
  try {
    committed = await readFile(outputPath)
  } catch (error) {
    if (error && typeof error === 'object' && 'code' in error && error.code === 'ENOENT') {
      console.error(`Missing generated OpenAPI types at ${outputPath}. Run pnpm api:generate and commit the result.`)
      process.exitCode = 1
      return
    }
    throw error
  }
  if (!committed.equals(Buffer.from(generated))) {
    console.error(`OpenAPI generated type drift detected in ${outputPath}. Run pnpm api:generate and commit the result.`)
    process.exitCode = 1
  } else {
    console.log('OpenAPI generated types are up to date.')
  }
}
