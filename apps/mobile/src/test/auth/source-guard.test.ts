import { readFileSync, readdirSync } from 'node:fs'
import { dirname, relative, resolve, sep } from 'node:path'
import { mockClerk, resetMockClerkAuth, setMockClerkAuth } from '../../../jest.setup'

/**
 * Source guard for the mobile auth seam.
 *
 * SCOPE — production source only: every `.ts`/`.tsx` file under `apps/mobile/app` and
 * `apps/mobile/src`, EXCLUDING `apps/mobile/src/test`, and NOT `apps/mobile/jest.setup.ts`.
 * An unscoped scan would flag this file's own samples, the setup file (which legitimately calls
 * `jest.mock` for Clerk and writes a type-only `import`), and the `dependency-policy` / `docs`
 * suites. `docs/clerk-auth-api.probe.ts` is outside both roots and is therefore not scanned.
 *
 * Why it exists: `@clerk/expo` transitively imports `@clerk/clerk-js`, so a top-level Clerk import
 * in the inert path would execute the browser bundle during the static export. The adapter keeps
 * Clerk behind a runtime-gated `require`, and this guard is what keeps it there.
 */

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const APP_DIR = resolve(MOBILE_ROOT, 'app')
const SRC_DIR = resolve(MOBILE_ROOT, 'src')
const TEST_DIR = resolve(SRC_DIR, 'test')

type SpecifierKind =
  | 'static-import'
  | 'export-from'
  | 'dynamic-import'
  | 'require'
  | 'require-resolve'

interface ModuleSpecifier {
  kind: SpecifierKind
  value: string
  index: number
}

function collectSourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = resolve(dir, entry.name)
    if (entry.isDirectory()) return collectSourceFiles(full)
    return /\.(ts|tsx)$/.test(entry.name) ? [full] : []
  })
}

function relativeToMobile(file: string): string {
  return relative(MOBILE_ROOT, file).split(sep).join('/')
}

/** Replaces comments with spaces, preserving length and newlines so indices stay aligned. */
function stripComments(source: string): string {
  type State = 'code' | 'single' | 'double' | 'template' | 'line' | 'block'
  let state: State = 'code'
  let out = ''
  let i = 0
  while (i < source.length) {
    const ch = source[i]
    const next = source[i + 1]
    if (state === 'code') {
      if (ch === '/' && next === '/') {
        state = 'line'
        out += '  '
        i += 2
        continue
      }
      if (ch === '/' && next === '*') {
        state = 'block'
        out += '  '
        i += 2
        continue
      }
      if (ch === "'") state = 'single'
      else if (ch === '"') state = 'double'
      else if (ch === '`') state = 'template'
      out += ch
      i += 1
      continue
    }
    if (state === 'line') {
      if (ch === '\n') {
        state = 'code'
        out += ch
      } else {
        out += ' '
      }
      i += 1
      continue
    }
    if (state === 'block') {
      if (ch === '*' && next === '/') {
        state = 'code'
        out += '  '
        i += 2
        continue
      }
      out += ch === '\n' ? ch : ' '
      i += 1
      continue
    }
    if (ch === '\\') {
      out += ch + (next ?? '')
      i += 2
      continue
    }
    if (
      (state === 'single' && ch === "'") ||
      (state === 'double' && ch === '"') ||
      (state === 'template' && ch === '`')
    ) {
      state = 'code'
    }
    out += ch
    i += 1
  }
  return out
}

const SPECIFIER_PATTERNS: ReadonlyArray<{ kind: SpecifierKind; pattern: RegExp }> = [
  { kind: 'require-resolve', pattern: /\brequire\.resolve\s*\(\s*(['"])([^'"]+)\1\s*\)/g },
  { kind: 'require', pattern: /\brequire\s*\(\s*(['"])([^'"]+)\1\s*\)/g },
  { kind: 'dynamic-import', pattern: /\bimport\s*\(\s*(['"])([^'"]+)\1\s*\)/g },
  {
    kind: 'export-from',
    pattern: /\bexport\s+(?:type\s+)?(?:\*|\{[^}]*\})\s*from\s*(['"])([^'"]+)\1/g,
  },
  {
    kind: 'static-import',
    pattern: /\bimport\s+(?:type\s+)?(?:[^'";]*?\bfrom\s+)?(['"])([^'"]+)\1/g,
  },
]

function extractSpecifiers(source: string): ModuleSpecifier[] {
  const stripped = stripComments(source)
  const found: ModuleSpecifier[] = []
  for (const { kind, pattern } of SPECIFIER_PATTERNS) {
    pattern.lastIndex = 0
    let match: RegExpExecArray | null
    while ((match = pattern.exec(stripped)) !== null) {
      found.push({ kind, value: match[2], index: match.index })
    }
  }
  return found.sort((a, b) => a.index - b.index)
}

function lineAt(source: string, index: number): string {
  const start = source.lastIndexOf('\n', index - 1) + 1
  const end = source.indexOf('\n', index)
  return source.slice(start, end === -1 ? source.length : end)
}

function braceDepthBetween(source: string, from: number, to: number): number {
  let depth = 0
  for (let i = from; i < to; i += 1) {
    if (source[i] === '{') depth += 1
    else if (source[i] === '}') depth -= 1
  }
  return depth
}

function routeFor(file: string): string | null {
  const segments = relative(APP_DIR, file).split(sep)
  const basename = segments.pop() ?? ''
  if (basename.startsWith('_') || basename.startsWith('+')) return null
  const stem = basename.replace(/\.(ts|tsx)$/, '')
  const groups = segments.filter((segment) => !/^\(.*\)$/.test(segment))
  return stem === 'index' ? `/${groups.join('/')}` : `/${[...groups, stem].join('/')}`
}

const productionFiles = [...collectSourceFiles(APP_DIR), ...collectSourceFiles(SRC_DIR)].filter(
  (file) => !file.startsWith(TEST_DIR + sep),
)

const CLERK_ALLOWLIST = new Set(['src/auth/mobile-auth.clerk.tsx', 'src/auth/token-cache.ts'])

describe('mobile auth source guard', () => {
  it('scans production source only, never the test tree or the jest setup file', () => {
    const rels = productionFiles.map(relativeToMobile)

    expect(rels).toContain('src/auth/mobile-auth.tsx')
    expect(rels).toContain('src/auth/mobile-auth.clerk.tsx')
    expect(rels.some((rel) => rel.startsWith('src/test/'))).toBe(false)
    expect(rels).not.toContain('jest.setup.ts')
  })

  it('detects every module-specifier form the guard forbids', () => {
    const sample = [
      "import { useAuth } from '@clerk/expo'",
      "export { x } from '@clerk/expo/experimental'",
      "const a = await import('@clerk/expo')",
      "const b = require('@clerk/expo')",
      "const c = require.resolve('@clerk/expo')",
    ].join('\n')

    const kinds = extractSpecifiers(sample)
      .filter((specifier) => specifier.value.startsWith('@clerk/'))
      .map((specifier) => specifier.kind)

    expect(new Set(kinds)).toEqual(
      new Set(['static-import', 'export-from', 'dynamic-import', 'require', 'require-resolve']),
    )
  })

  it('allows a @clerk/ specifier only in the two allowlisted seam files', () => {
    const violations = productionFiles.flatMap((file) => {
      const rel = relativeToMobile(file)
      if (CLERK_ALLOWLIST.has(rel)) return []
      return extractSpecifiers(readFileSync(file, 'utf8'))
        .filter((specifier) => specifier.value.startsWith('@clerk/'))
        .map((specifier) => `${rel}: ${specifier.kind} ${specifier.value}`)
    })

    expect(violations).toEqual([])
  })

  it('keeps src/auth/mobile-auth.tsx free of every @clerk/ reference', () => {
    const source = readFileSync(resolve(SRC_DIR, 'auth/mobile-auth.tsx'), 'utf8')

    expect(source.includes('@clerk/')).toBe(false)
    expect(
      extractSpecifiers(source).filter((specifier) => specifier.value.startsWith('@clerk/')),
    ).toEqual([])
  })

  it('references mobile-auth.clerk from no production file except mobile-auth.tsx', () => {
    const offenders = productionFiles
      .map(relativeToMobile)
      .filter((rel) => rel !== 'src/auth/mobile-auth.tsx')
      .filter((rel) => readFileSync(resolve(MOBILE_ROOT, rel), 'utf8').includes('mobile-auth.clerk'))

    expect(offenders).toEqual([])
  })

  it('reaches mobile-auth.clerk from mobile-auth.tsx only through a gated, non-top-level require', () => {
    const rel = 'src/auth/mobile-auth.tsx'
    const source = readFileSync(resolve(MOBILE_ROOT, rel), 'utf8')
    const references = extractSpecifiers(source).filter((specifier) =>
      specifier.value.includes('mobile-auth.clerk'),
    )

    expect(references).toHaveLength(1)
    expect(references[0].kind).toBe('require')

    // A static import or export-from would execute Clerk at module load, so the reference must be
    // an indented statement, not a top-level one.
    const reference = references[0]
    const line = lineAt(source, reference.index)
    expect(line).not.toBe(line.trimStart())

    // ...and it must sit INSIDE the `authAvailable && publishableKey` branch, whose block is still
    // open at the require (brace depth > 0).
    const gateIndex = source.indexOf('if (authAvailable && publishableKey) {')
    expect(gateIndex).toBeGreaterThanOrEqual(0)
    expect(reference.index).toBeGreaterThan(gateIndex)
    expect(braceDepthBetween(source, gateIndex, reference.index)).toBeGreaterThan(0)
  })

  it('reaches @clerk/expo/token-cache from token-cache.ts only through a require', () => {
    const rel = 'src/auth/token-cache.ts'
    const source = readFileSync(resolve(MOBILE_ROOT, rel), 'utf8')
    const references = extractSpecifiers(source).filter(
      (specifier) => specifier.value === '@clerk/expo/token-cache',
    )

    expect(references).toHaveLength(1)
    expect(references[0].kind).toBe('require')
    expect(lineAt(source, references[0].index)).not.toBe(lineAt(source, references[0].index).trimStart())
  })

  it('resolves only app/index.tsx to the root route', () => {
    const offenders = collectSourceFiles(APP_DIR)
      .map((file) => ({ file: relativeToMobile(file), route: routeFor(file) }))
      .filter((entry) => entry.route === '/')
      .map((entry) => entry.file)

    expect(offenders).toEqual(['app/index.tsx'])
  })

  it('installs the global @clerk/expo mock so no unit test loads the real module', () => {
    const setup = readFileSync(resolve(MOBILE_ROOT, 'jest.setup.ts'), 'utf8')

    expect(setup).toContain("jest.mock('@clerk/expo'")
    expect(setup).toContain("typeof import('@clerk/expo')")
    expect(setup).toContain("jest.mock('@clerk/expo/token-cache'")
  })

  it('resolves @clerk/expo to the controllable global mock, not the real module', () => {
    const clerkExpo = jest.requireMock('@clerk/expo') as { useAuth: () => unknown }
    const readAuth = clerkExpo.useAuth

    expect(readAuth()).toBe(mockClerk().auth)

    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })
    expect(mockClerk().auth.userId).toBe('user_1')
    expect(readAuth()).toMatchObject({ isSignedIn: true, userId: 'user_1' })

    resetMockClerkAuth()
    expect(mockClerk().auth).toMatchObject({ isLoaded: true, isSignedIn: false, userId: null })
  })
})
