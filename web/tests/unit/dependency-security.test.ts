// @vitest-environment node
import { generateKeyPairSync } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import { describe, expect, it } from 'vitest'

function dependency(ancestors: string[], name: string) {
  const require = ancestors.reduce(
    (parent, ancestor) => createRequire(parent.resolve(ancestor)),
    createRequire(import.meta.url),
  )
  return require(name)
}

const nitro = ['nuxt/package.json', '@nuxt/nitro-server/package.json', 'nitropack/package.json']
const braces = dependency([...nitro, 'globby', 'fast-glob', 'micromatch'], 'braces')

it('copies security patches into the web image before installing dependencies', () => {
  const dockerfile = readFileSync(new URL('../../Dockerfile', import.meta.url), 'utf8')
  expect(dockerfile).toMatch(/COPY[^\n]* patches\/ \.\/patches\/[\s\S]*pnpm install --frozen-lockfile/)
})

it('loads the Nuxt DevTools consumer with the patched simple-git major', async () => {
  const require = createRequire(import.meta.url)
  const nuxtRequire = createRequire(require.resolve('nuxt/package.json'))
  const main = nuxtRequire.resolve('@nuxt/devtools/dist/chunks/module-main.mjs')
  await expect(import(pathToFileURL(main).href)).resolves.toHaveProperty('m.enableModule', expect.any(Function))
})

describe('braces recursion bounds (CVE-2026-93687)', () => {
  for (const method of ['parse', 'compile', 'expand', 'stringify'] as const) {
    it(`${method} rejects deeply nested braces and parentheses before walking the AST`, () => {
      for (const [open, close] of [['{', '}'], ['(', ')'], ['{(', ')}']] as const) {
        const pattern = open.repeat(2000) + 'a,b' + close.repeat(2000)
        expect(() => braces[method](pattern)).toThrow(SyntaxError)
        expect(() => braces[method](pattern)).toThrow('Nesting depth exceeds maximum of 100')
      }
    })
  }

  it('preserves ordinary expansion and nesting up to the bound', () => {
    expect(braces.expand('src/{app,lib}/**/*.{ts,vue}')).toEqual([
      'src/app/**/*.ts', 'src/app/**/*.vue', 'src/lib/**/*.ts', 'src/lib/**/*.vue',
    ])
    expect(braces.expand('{'.repeat(100) + 'a,b' + '}'.repeat(100))).toEqual([
      '{'.repeat(99) + 'a' + '}'.repeat(99),
      '{'.repeat(99) + 'b' + '}'.repeat(99),
    ])
    expect(() => braces('{'.repeat(101) + 'a,b' + '}'.repeat(101))).toThrow(SyntaxError)
    expect(braces.expand('{a,b}'.repeat(2))).toEqual(['aa', 'ab', 'ba', 'bb'])
    expect(braces.compile('\\{'.repeat(200))).toBe('{'.repeat(200))
  })
})

describe('node-forge DigestAlgorithm validation (CVE-2026-85393)', () => {
  const keys = generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicExponent: 3,
    privateKeyEncoding: { type: 'pkcs1', format: 'pem' },
    publicKeyEncoding: { type: 'pkcs1', format: 'pem' },
  })

  for (const ancestors of [['nuxt/package.json', '@nuxt/cli', 'listhen'], [...nitro, 'listhen']]) {
    const forge = dependency(ancestors, 'node-forge')
    const publicKey = forge.pki.publicKeyFromPem(keys.publicKey)
    const privateKey = forge.pki.privateKeyFromPem(keys.privateKey)
    const { asn1 } = forge
    const digest = forge.md.sha256.create().update('dependency security regression').digest().getBytes()

    for (const includeNull of [false, true]) {
      it(`rejects extra algorithm elements via ${ancestors.join(' > ')} (NULL: ${includeNull})`, () => {
        const algorithm = asn1.create(asn1.Class.UNIVERSAL, asn1.Type.SEQUENCE, true, [
          asn1.create(asn1.Class.UNIVERSAL, asn1.Type.OID, false, asn1.oidToDer(forge.oids.sha256).getBytes()),
          ...(includeNull ? [asn1.create(asn1.Class.UNIVERSAL, asn1.Type.NULL, false, '')] : []),
        ])
        const digestInfo = asn1.create(asn1.Class.UNIVERSAL, asn1.Type.SEQUENCE, true, [
          algorithm,
          asn1.create(asn1.Class.UNIVERSAL, asn1.Type.OCTETSTRING, false, digest),
        ])
        const sign = () => privateKey.sign(asn1.toDer(digestInfo).getBytes(), 'NONE')

        expect(publicKey.verify(digest, sign())).toBe(true)
        algorithm.value.push(asn1.create(asn1.Class.UNIVERSAL, asn1.Type.OCTETSTRING, false, 'garbage'))
        expect(() => publicKey.verify(digest, sign())).toThrow('DigestInfo value')
      })
    }
  }
})
