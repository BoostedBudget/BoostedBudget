import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

/**
 * Todo 6 record guard (#1658).
 *
 * The NativeWind RC verdict lives in a prose ADR
 * (`docs/decisions/2026-09-19-nativewind-v5-rc-adoption.md`), which nothing else
 * keeps honest. This suite is the mechanism that does, and it enforces two
 * contracts the ADR cannot be allowed to drift from:
 *
 *   (i)  the versions the ADR records equal the pins actually declared in
 *        `apps/mobile/package.json`, so the ADR cannot go stale silently; and
 *   (ii) the verdict is DERIVED from the machine-written evidence file, not
 *        authored — `evidence.items.every(item => item.pass)` must map to
 *        `KEEP_RC` when every item passed and `RETAIN` otherwise. A worker
 *        therefore cannot write `KEEP_RC` over a failed measurement.
 *
 * It also pins the load-bearing surrounding facts the plan's acceptance names:
 * all thirteen measured items are recorded in the ADR, the word `rollback`
 * appears (the rollback path is not optional), and the three open engine issues
 * are cited as known risks (the ADR must not claim "no known issues").
 */

const MOBILE_ROOT = dirname(require.resolve('../../package.json'))
const REPO_ROOT = resolve(MOBILE_ROOT, '..', '..')
const PACKAGE_PATH = resolve(MOBILE_ROOT, 'package.json')
const EVIDENCE_PATH = resolve(MOBILE_ROOT, 'nativewind-gate.evidence.json')
const ADR_PATH = resolve(REPO_ROOT, 'docs/decisions/2026-09-19-nativewind-v5-rc-adoption.md')

interface GateItem {
  id: string
  measured: number | boolean | null
  threshold: unknown
  pass: boolean
}

interface GateEvidence {
  items: GateItem[]
}

function readJson<T>(path: string): T {
  return JSON.parse(readFileSync(path, 'utf8')) as T
}

const mobilePackage = readJson<{ dependencies: Record<string, string> }>(PACKAGE_PATH)
const evidence = readJson<GateEvidence>(EVIDENCE_PATH)
const adr = readFileSync(ADR_PATH, 'utf8')

// The one machine-readable verdict declaration. Everything else in the ADR may
// discuss the vocabulary; this line is what the test treats as the verdict.
const VERDICT_LINE = /^\*\*Verdict:\*\*\s+(\S+)\s*$/m

function declaredVerdict(): string | undefined {
  return VERDICT_LINE.exec(adr)?.[1]
}

// The "Recorded version pins" table row is `| \`nativewind\` | \`5.0.0-rc.0\` | … |`.
// Capturing the first backticked cell after the package name is enough to tie
// the ADR to the declared pin; a prose mention is not followed by a `|`.
function recordedPin(packageName: string): string | undefined {
  const pattern = new RegExp('`' + packageName + '`\\s*\\|\\s*`([^`]+)`')
  return pattern.exec(adr)?.[1]
}

describe('NativeWind RC verdict record', () => {
  it('records the declared nativewind pin that package.json actually declares', () => {
    expect(recordedPin('nativewind')).toBe(mobilePackage.dependencies.nativewind)
  })

  it('records the declared react-native-css pin that package.json actually declares', () => {
    expect(recordedPin('react-native-css')).toBe(
      mobilePackage.dependencies['react-native-css'],
    )
  })

  it('records every measured evidence item in the ADR', () => {
    for (const item of evidence.items) {
      expect(adr).toContain(item.id)
    }
  })

  it('derives KEEP_RC from every item passing, and RETAIN from any failure', () => {
    const everyItemPassed = evidence.items.every((item) => item.pass)
    expect(declaredVerdict()).toBe(everyItemPassed ? 'KEEP_RC' : 'RETAIN')
  })

  it('declares exactly one of the two allowed verdict tokens', () => {
    expect(['KEEP_RC', 'RETAIN']).toContain(declaredVerdict())
  })

  it('never uses PROMOTE as the verdict token', () => {
    expect(adr).not.toMatch(/^\*\*Verdict:\*\*\s+PROMOTE\s*$/m)
  })

  it('records an explicit rollback path', () => {
    expect(adr.toLowerCase()).toContain('rollback')
  })

  it('cites the open engine issues as known risks instead of claiming none', () => {
    expect(adr).toContain('#386')
    expect(adr).toContain('#453')
    expect(adr).toContain('#454')
  })
})
