import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * Desktop-stability regression guard (#1658, Todo 23).
 *
 * `apps/web/src/index.css:3` imports `@boostedbudget/design-tokens/web.css`, so
 * ANY byte that moves in the generated web stylesheet is a desktop appearance
 * change. The mobile work in #1658 is additive-only; this is the canonical guard
 * that proves the desktop token surface did not move.
 *
 * ⚠ Do NOT `import` `renderWebCss` to compare against the fixture:
 * `scripts/generate.ts` runs `writeFileSync` at module top level when neither
 * `--stdout` nor `--check` is in `argv`, so importing it REGENERATES the
 * committed files as a side effect. Spawn `--stdout` instead, mirroring
 * `generated-css.test.ts` and `mobile-scales.test.ts`.
 */

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(packageRoot, '..', '..')
const webPath = resolve(packageRoot, 'generated/web.css')
const fixturePath = resolve(packageRoot, 'test/fixtures/web.css.pre-1658')
const indexCssPath = resolve(repoRoot, 'apps/web/src/index.css')

/** The exact specifier `apps/web/src/index.css:3` must keep importing. */
const WEB_TOKEN_IMPORT = '@import "@boostedbudget/design-tokens/web.css";'

function read(path: string): string {
  return readFileSync(path, 'utf8')
}

/** Runs the generator with `--stdout` and returns only the web.css slice. */
function generateWebCss(): string {
  const result = Bun.spawnSync({
    cmd: [process.execPath, 'run', 'scripts/generate.ts', '--stdout'],
    cwd: packageRoot,
  })
  expect(result.exitCode).toBe(0)

  const stdout = result.stdout.toString()
  const separator = '\n--- nativewind.css ---\n'
  const separatorIndex = stdout.indexOf(separator)
  expect(separatorIndex).toBeGreaterThan(-1)
  return stdout.slice(0, separatorIndex)
}

describe('desktop token surface stability', () => {
  it('generates web.css byte-identical to the pre-change fixture', () => {
    // The generator's fresh output must not have moved. A subset/presence check
    // cannot catch new keys added to `:root`, so this is whole-file equality.
    expect(generateWebCss()).toBe(read(fixturePath))
  })

  it('keeps the committed generated/web.css byte-identical to the fixture', () => {
    // Independent of the generator run above: the artifact actually shipped to
    // `apps/web` (and to `git`) must also be unchanged, so a hand-edit that is
    // not regenerated cannot slip past the guard.
    expect(read(webPath)).toBe(read(fixturePath))
  })

  it('keeps apps/web/src/index.css importing the web token stylesheet by exact specifier', () => {
    expect(read(indexCssPath)).toContain(WEB_TOKEN_IMPORT)
  })

  it('keeps the explicit .dark selector and no system dark mode in generated/web.css', () => {
    const web = read(webPath)
    expect(web).toContain('.dark {')
    expect(web).not.toContain('prefers-color-scheme')
  })
})
