import { describe, expect, it } from 'bun:test'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { nativeTokens } from '../generated/native'

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const excludePath = resolve(packageRoot, 'generated/nativewind-exclude.json')

function readExclude(): string[] {
  return JSON.parse(readFileSync(excludePath, 'utf8')) as string[]
}

function adapterKeys(theme: 'light' | 'dark'): string[] {
  return [...Object.keys(nativeTokens[theme].color), ...Object.keys(nativeTokens[theme].contrast)]
}

/** The compiler's `InlineVariableOptions.exclude` entries are `--`-prefixed; adapter keys are not. */
function normalise(name: string): string {
  return name.replace(/^--/, '')
}

describe('generated nativewind exclude list', () => {
  it('exists as a JSON array of --prefixed names', () => {
    expect(existsSync(excludePath)).toBe(true)

    const parsed = readExclude()
    expect(Array.isArray(parsed)).toBe(true)
    expect(parsed.length).toBeGreaterThan(0)
    for (const name of parsed) {
      expect(typeof name).toBe('string')
      expect(name.startsWith('--')).toBe(true)
    }
  })

  it('lists every theme-driven variable exactly once', () => {
    const parsed = readExclude()
    expect(new Set(parsed).size).toBe(parsed.length)
  })

  it('matches the generated adapter key set with the -- prefix normalised on both sides', () => {
    const parsed = readExclude().map(normalise).sort()

    expect(parsed).toEqual(adapterKeys('light').map(normalise).sort())
    expect(parsed).toEqual(adapterKeys('dark').map(normalise).sort())
  })
})