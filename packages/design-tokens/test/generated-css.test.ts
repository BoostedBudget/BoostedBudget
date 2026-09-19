import { describe, expect, it } from 'bun:test'
import { cpSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const webPath = resolve(packageRoot, 'generated/web.css')
const nativewindPath = resolve(packageRoot, 'generated/nativewind.css')
const nativePath = resolve(packageRoot, 'generated/native.ts')

function readGenerated(path: string): string {
  return existsSync(path) ? readFileSync(path, 'utf8') : ''
}

function extractTokens(css: string, selector: string): Record<string, string> {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = css.match(new RegExp(`${escapedSelector}\\s*\\{([^}]+)\\}`))
  if (!match) return {}

  return Object.fromEntries(
    [...match[1].matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)].map((token) => [
      token[1],
      token[2].trim(),
    ]),
  )
}

describe('generated design-token CSS', () => {
  it('is reproducible from the typed token source', () => {
    const first = Bun.spawnSync({
      cmd: [process.execPath, 'run', 'scripts/generate.ts', '--stdout'],
      cwd: packageRoot,
    })
    const second = Bun.spawnSync({
      cmd: [process.execPath, 'run', 'scripts/generate.ts', '--stdout'],
      cwd: packageRoot,
    })

    expect(first.exitCode).toBe(0)
    expect(second.exitCode).toBe(0)
    expect(first.stdout.toString()).toBe(second.stdout.toString())
    expect(first.stdout.toString()).toBe(
      `${readGenerated(webPath)}\n--- nativewind.css ---\n${readGenerated(nativewindPath)}\n--- native.ts ---\n${readGenerated(nativePath)}`,
    )
  })

  it('keeps light and dark semantic token values in parity', () => {
    const web = readGenerated(webPath)
    const nativewind = readGenerated(nativewindPath)
    const webLight = extractTokens(web, ':root')
    const webDark = extractTokens(web, '.dark')
    const nativeBlocks = [...nativewind.matchAll(/:root\s*\{([^}]+)\}/g)]

    expect(Object.keys(webLight).length).toBeGreaterThan(0)
    expect(nativeBlocks).toHaveLength(2)

    const nativeLight = extractTokens(nativewind, ':root')
    const nativeDark = Object.fromEntries(
      [...nativeBlocks[1][1].matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)].map((token) => [
        token[1],
        token[2].trim(),
      ]),
    )

    expect(nativeLight).toEqual(webLight)
    expect(nativeDark).toEqual(webDark)
  })

  it('keeps manual web dark mode separate from native system dark mode', () => {
    const web = readGenerated(webPath)
    const nativewind = readGenerated(nativewindPath)

    expect(web).toContain('.dark {')
    expect(web).not.toContain('prefers-color-scheme')
    expect(nativewind).toContain('@media (prefers-color-scheme: dark)')
    expect(nativewind).not.toContain('.dark {')
  })

  it('reports a stale NativeWind artifact', () => {
    const fixtureDir = mkdtempSync(resolve(tmpdir(), 'boostedbudget-design-tokens-'))
    try {
      cpSync(resolve(packageRoot, 'generated'), fixtureDir, { recursive: true })
      writeFileSync(resolve(fixtureDir, 'nativewind.css'), 'stale native tokens\n')

      const result = Bun.spawnSync({
        cmd: [
          process.execPath,
          'run',
          'scripts/generate.ts',
          '--check',
          '--generated-dir',
          fixtureDir,
        ],
        cwd: packageRoot,
      })

      expect(result.exitCode).toBe(1)
      expect(result.stderr.toString()).toContain('nativewind.css is stale')
    } finally {
      rmSync(fixtureDir, { recursive: true, force: true })
    }
  })

  it('reports a stale native adapter artifact', () => {
    const fixtureDir = mkdtempSync(resolve(tmpdir(), 'boostedbudget-design-tokens-'))
    try {
      cpSync(resolve(packageRoot, 'generated'), fixtureDir, { recursive: true })
      writeFileSync(resolve(fixtureDir, 'native.ts'), 'stale native tokens\n')

      const result = Bun.spawnSync({
        cmd: [
          process.execPath,
          'run',
          'scripts/generate.ts',
          '--check',
          '--generated-dir',
          fixtureDir,
        ],
        cwd: packageRoot,
      })

      expect(result.exitCode).toBe(1)
      expect(result.stderr.toString()).toContain('native.ts is stale')
    } finally {
      rmSync(fixtureDir, { recursive: true, force: true })
    }
  })
})
