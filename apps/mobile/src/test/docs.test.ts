import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

/**
 * Todo 25 documentation guard (#1658).
 *
 * The mobile README is the migration contract feature teams read, and nothing
 * else keeps it honest. This suite enforces three contracts the prose cannot be
 * allowed to drift from:
 *
 *   (i)  #1658 is DELIVERED, so it must no longer appear in the
 *        `## Deliberately deferred` section — while the migration and
 *        platform-exception sections must still name it, so the scope mapping
 *        stays auditable. A bare `grep -c "1658"` cannot express "no longer
 *        deferred", so the check is STRUCTURAL: it parses the section between
 *        its heading and the next `## ` heading.
 *   (ii) the `nativewind` version the README documents equals the pin
 *        `apps/mobile/package.json` actually declares, so the docs cannot go
 *        stale silently.
 *   (iii) the design-tokens README names all three generated artifacts, so a
 *        reader can find every adapter.
 */

const MOBILE_ROOT = dirname(require.resolve('../../package.json'))
const REPO_ROOT = resolve(MOBILE_ROOT, '..', '..')
const MOBILE_README_PATH = resolve(MOBILE_ROOT, 'README.md')
const DESIGN_TOKENS_README_PATH = resolve(REPO_ROOT, 'packages/design-tokens/README.md')
const MOBILE_PACKAGE_PATH = resolve(MOBILE_ROOT, 'package.json')

const mobileReadme = readFileSync(MOBILE_README_PATH, 'utf8')
const designTokensReadme = readFileSync(DESIGN_TOKENS_README_PATH, 'utf8')
const mobilePackage = JSON.parse(readFileSync(MOBILE_PACKAGE_PATH, 'utf8')) as {
  dependencies: Record<string, string>
}

/**
 * Returns the body of a `## <heading>` section: everything after the heading
 * line up to (but excluding) the next `## ` heading. Throws when the heading is
 * absent, so a renamed section fails loudly rather than silently passing.
 */
function section(readme: string, heading: string): string {
  const lines = readme.split('\n')
  const start = lines.findIndex((line) => line.trim() === `## ${heading}`)
  if (start === -1) throw new Error(`README is missing the "## ${heading}" section`)
  const rest = lines.slice(start + 1)
  const end = rest.findIndex((line) => /^##\s/.test(line))
  return (end === -1 ? rest : rest.slice(0, end)).join('\n')
}

/** Counts top-level markdown list items in a section body. */
function countListItems(body: string): number {
  return body.split('\n').filter((line) => /^\s*[-*]\s+\S/.test(line)).length
}

describe('mobile README documentation contract', () => {
  it('no longer defers #1658 in the Deliberately deferred section', () => {
    expect(section(mobileReadme, 'Deliberately deferred')).not.toContain('1658')
  })

  it('names #1658 in the migration guidance section', () => {
    expect(section(mobileReadme, 'Migration guidance')).toContain('1658')
  })

  it('names #1658 in the platform exceptions section', () => {
    expect(section(mobileReadme, 'Platform exceptions')).toContain('1658')
  })

  it('documents all three design-token adapters', () => {
    expect(mobileReadme).toContain('web.css')
    expect(mobileReadme).toContain('nativewind.css')
    expect(mobileReadme).toContain('@boostedbudget/design-tokens/native')
  })

  it('documents the theme key and the user-scoped seam', () => {
    expect(mobileReadme).toContain('bb-theme')
    expect(mobileReadme).toContain('bb-theme:<userId>')
  })

  it('documents at least five platform exceptions', () => {
    expect(countListItems(section(mobileReadme, 'Platform exceptions'))).toBeGreaterThanOrEqual(5)
  })

  it('documents a non-empty migration section', () => {
    expect(section(mobileReadme, 'Migration guidance').trim().length).toBeGreaterThan(0)
  })

  it('documents the nativewind version that package.json declares', () => {
    const pinMatch = /`nativewind`[^\n]*?`(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)`/.exec(mobileReadme)
    expect(pinMatch).not.toBeNull()
    expect(pinMatch![1]).toBe(mobilePackage.dependencies.nativewind)
  })
})

describe('design-tokens README documentation contract', () => {
  it('names all three generated artifacts', () => {
    expect(designTokensReadme).toContain('web.css')
    expect(designTokensReadme).toContain('nativewind.css')
    expect(designTokensReadme).toContain('native.ts')
  })
})