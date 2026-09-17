import { afterEach, describe, expect, test } from 'bun:test'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { normalizeGeneratedOutput } from './normalize-generated-output.mjs'

let fixtureRoot: string | undefined

afterEach(async () => {
  if (fixtureRoot) await rm(fixtureRoot, { recursive: true, force: true })
  fixtureRoot = undefined
})

describe('normalizeGeneratedOutput', () => {
  test('makes generated TypeScript files end in exactly one newline', async () => {
    fixtureRoot = await mkdtemp(join(tmpdir(), 'orval-output-'))
    const generated = join(fixtureRoot, 'endpoint.ts')
    const untouched = join(fixtureRoot, 'README.md')
    await writeFile(generated, 'export const endpoint = true\n\n\n')
    await writeFile(untouched, 'keep markdown whitespace\n\n')

    await normalizeGeneratedOutput(fixtureRoot)

    expect(await readFile(generated, 'utf8')).toBe('export const endpoint = true\n')
    expect(await readFile(untouched, 'utf8')).toBe('keep markdown whitespace\n\n')
  })
})
