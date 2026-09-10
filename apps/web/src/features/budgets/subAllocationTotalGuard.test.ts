/**
 * Issue #1630 task 3 — Guard: every `.reduce()` over allocated_amount/spent_amount in the budgets
 * feature must reduce over filterTopLevel's output, or be on this list with a reason.
 *
 * THE BUG THIS GUARD EXISTS TO STOP FROM RECURRING. Seven places in this codebase compute a
 * budget's total allocated/spent amount by summing budget_categories rows. Only two of them
 * (backend domain.Budget.TotalSpent/TotalAllocated, and this feature's own computeBudgetMetrics)
 * originally excluded sub-allocations — a child category nested under a budgeted parent must be
 * left out of the sum, because the parent's own amount already rolls the child up (#1203), refined
 * by #1630 to also gate on match_logic containment (see isContainingMatchLogic/
 * isContainedMatchLogic in ./types.ts). The other five double-counted every nested child; FOUR of
 * those five were `.reduce()` calls in this feature's own components
 * (AllocationBreakdown.tsx, BudgetCategoryManager.tsx ×2, BudgetDetailPage.tsx) — invisible to a
 * backend-only guard, which is precisely why this half exists. Its Go sibling,
 * backend/internal/repository/sub_allocation_total_guard_test.go, covers the backend SQL half;
 * neither guard can see what the other protects.
 *
 * THE RULE. A `.reduce()` whose callback body references `allocated_amount` or `spent_amount` is
 * safe only when it reduces over the result of `filterTopLevel(...)` — either directly
 * (`filterTopLevel(categories).reduce(...)`) or through a local variable assigned from it earlier
 * in the same file (`const topLevel = filterTopLevel(categories); ...; topLevel.reduce(...)`).
 * Anything else is either wrong (it needs filterTopLevel) or right for a reason this guard cannot
 * see, and either way a human must say which, in allowedRawSubAllocationReduces, with a reason.
 *
 * WHY A TEXT SCAN, NOT A TYPE-AWARE ANALYSIS. Same tradeoff every other guard in this repo makes
 * (see backend's history_cascade_guard_test.go and history_delete_guard_test.go): a real
 * type-flow analysis would need a TS type checker to prove a variable's value truly came from
 * filterTopLevel; a text scan cannot prove that, only that the file at least SPELLS one of the two
 * known-good shapes. The behavioural authority remains types.test.ts's computeBudgetMetrics
 * coverage and each component's own over-allocation tests; this guard's job is narrower and
 * cheaper: it runs on every `bun run test`, with no backend or database involved, and it means the
 * FIFTH wrong `.reduce()` never ships silently the way the first four did.
 *
 * MUTATION-CHECKED. The meta-tests below construct the exact violation (a `.reduce()` over the raw
 * `categories` array referencing `allocated_amount`, with no `filterTopLevel` anywhere) and prove
 * the pure decision procedure (subAllocationReduceVerdict) reports it. Separately, a real
 * `.reduce()` in AllocationBreakdown.tsx was temporarily pointed at the raw `categories` prop
 * instead of `topLevel`, and the live guard (subAllocationReduceGuard) was re-run against the
 * corpus: it went RED, naming that file, before the substitution was reverted. See
 * task-3-report.md for the transcript.
 */
import { describe, it, expect } from 'bun:test'
import { readdirSync, readFileSync, statSync } from 'fs'
import { join, relative } from 'path'

const BUDGETS_ROOT = join(import.meta.dir) // apps/web/src/features/budgets

// ---- Pure extraction & decision — no filesystem, so the meta-tests can construct a violation
// ---- with a bare string and no fixture file. ------------------------------------------------

const SUB_ALLOCATION_COLUMN = /\b(?:allocated_amount|spent_amount)\b/

/** forwardMatchCloseParen returns the index of the ')' that closes the '(' at openParenIndex,
 * counting nested parens (an arrow-function callback's own `(sum, cat) => ...` parens included) so
 * a reduce callback is captured exactly at ITS closing paren, not cut short by, or run past, one
 * belonging to something else in the same expression. -1 if unbalanced. */
function forwardMatchCloseParen(content: string, openParenIndex: number): number {
  let depth = 1
  for (let i = openParenIndex + 1; i < content.length; i++) {
    if (content[i] === '(') depth++
    else if (content[i] === ')') {
      depth--
      if (depth === 0) return i
    }
  }
  return -1
}

function normalizeWhitespace(s: string): string {
  return s.replace(/\s+/g, ' ').trim()
}

const IDENT_CHAR = /[A-Za-z0-9_$]/

/**
 * backwardBaseReceiver walks backward from `idx` (the position right before `.reduce(`) over the
 * receiver EXPRESSION CHAIN — using real balanced-paren counting, not a regexp, because a regexp
 * bounded only by "no semicolon" cannot tell a call's own parens apart from an enclosing one (an
 * earlier draft of this guard used `[^;]*\)\s*$` and it ran straight through
 * `useMemo(() => filterTopLevel(categories), [categories])`'s wrapping call, capturing `useMemo(...`
 * as the receiver of a `.reduce()` two statements later and missing that it was rooted in
 * filterTopLevel at all).
 *
 * Peels the chain one call/member segment at a time:
 *   - a call ending in `)` — find its balanced-matching `(`, then the identifier immediately
 *     before it. If that identifier is `filterTopLevel`, the whole chain is rooted there and we
 *     stop immediately (whatever `.filter(...)`/`.map(...)` runs between it and `.reduce(` only
 *     narrows an already-top-level set, so it stays safe). If instead that identifier is preceded
 *     by a `.` (a chained method call, e.g. `.filter(...)`), keep peeling past it. Otherwise it's
 *     an unrelated base call (e.g. `someHelper(...).reduce(...)`) and IS the base.
 *   - a bare identifier/property-access run (`topLevel`, `budget.categories`) — that run IS the
 *     base.
 */
function backwardBaseReceiver(content: string, idx: number): string {
  let i = idx
  for (;;) {
    while (i > 0 && /\s/.test(content[i - 1])) i-- // a chain may break across lines, e.g.
    // `topLevel\n  .filter(...)\n  .reduce(...)` — whitespace between segments is not itself
    // part of the receiver and must not end the walk.
    if (i <= 0) return content.slice(0, idx).trim()
    const ch = content[i - 1]
    if (ch === ')') {
      let depth = 1
      let j = i - 2
      while (j >= 0 && depth > 0) {
        if (content[j] === ')') depth++
        else if (content[j] === '(') depth--
        if (depth > 0) j--
      }
      if (depth !== 0) return content.slice(0, idx).trim() // unbalanced — bail conservatively
      const openParenIdx = j
      let k = openParenIdx - 1
      while (k >= 0 && IDENT_CHAR.test(content[k])) k--
      const name = content.slice(k + 1, openParenIdx)
      if (name === 'filterTopLevel') return 'filterTopLevel(...)'
      if (k >= 0 && content[k] === '.') {
        i = k // continue peeling from just before the dot
        continue
      }
      return `${name}(...)`
    }
    if (IDENT_CHAR.test(ch) || ch === '.') {
      let k = i - 1
      while (k >= 0 && (IDENT_CHAR.test(content[k]) || content[k] === '.')) k--
      return content.slice(k + 1, i)
    }
    return content.slice(Math.max(0, idx - 40), idx).trim()
  }
}

export interface ReduceSite {
  /** Stable identifying text for this site — the normalized `<receiver>.reduce(<body...>` snippet,
   * NOT a line number (CLAUDE.md: "line numbers rot"; a file-level or line-keyed allowlist would
   * let an unrelated edit silently re-point or orphan an entry). */
  key: string
  /** The expression `.reduce` was called on, e.g. `topLevel` or `filterTopLevel(categories)`. */
  receiver: string
  /** The full argument list text passed to `.reduce(...)`. */
  body: string
}

/**
 * reduceSitesIn extracts every `<receiver>.reduce(<body>)` call in `content` whose callback body
 * mentions allocated_amount or spent_amount. Pure: operates on a string, no filesystem.
 */
export function reduceSitesIn(content: string): ReduceSite[] {
  const sites: ReduceSite[] = []
  const marker = '.reduce('
  let searchFrom = 0
  for (;;) {
    const idx = content.indexOf(marker, searchFrom)
    if (idx === -1) break
    const openParen = idx + marker.length - 1
    const closeParen = forwardMatchCloseParen(content, openParen)
    if (closeParen === -1) {
      searchFrom = idx + marker.length
      continue
    }
    searchFrom = closeParen + 1
    const body = content.slice(openParen + 1, closeParen)
    if (!SUB_ALLOCATION_COLUMN.test(body)) continue

    // The base of the receiver chain — see backwardBaseReceiver's doc comment for why this is a
    // real balanced-paren walk and not a regexp. It never affects which reduce calls are FOUND,
    // only which ones are judged safe.
    const receiver = backwardBaseReceiver(content, idx)

    const key = normalizeWhitespace(`${receiver}.reduce(${body}`).slice(0, 160)
    sites.push({ key, receiver, body })
  }
  return sites
}

/**
 * isReceiverSafe reports whether `receiver` is provably filterTopLevel's output: either the
 * receiver chain is rooted directly in a `filterTopLevel(...)` call (backwardBaseReceiver already
 * resolved that to the literal `'filterTopLevel(...)'`), or `receiver` is a bare identifier whose
 * declaration's initializer — the same `const`/`let` statement, bounded by the next semicolon so
 * an unrelated LATER statement's `filterTopLevel(...)` cannot satisfy an EARLIER variable — calls
 * filterTopLevel anywhere inside it. The `[^;]*?` gap (not `=\s*filterTopLevel\(` directly) is
 * deliberate: AllocationBreakdown.tsx assigns `const topLevel = useMemo(() => filterTopLevel(
 * categories), [categories])`, one level of indirection an exact-adjacency check would miss.
 */
export function isReceiverSafe(receiver: string, fileContent: string): boolean {
  if (receiver === 'filterTopLevel(...)') return true
  if (!/^[A-Za-z_$][\w$]*$/.test(receiver)) return false // a member-access chain, not a bare var
  const escaped = receiver.replace(/[$]/g, '\\$')
  const assignedFromFilterTopLevel = new RegExp(
    `\\b(?:const|let)\\s+${escaped}\\s*=[^;]*?\\bfilterTopLevel\\(`
  )
  return assignedFromFilterTopLevel.test(fileContent)
}

/**
 * subAllocationReduceVerdict is the whole decision procedure, kept pure so the meta-tests can
 * construct the violation each direction exists to catch. `sitesByFile` maps a reporting key (the
 * relative file path) to that file's raw content (for isReceiverSafe's variable-assignment search)
 * and its extracted sites.
 */
export function subAllocationReduceVerdict(
  filesToSites: { file: string; content: string; sites: ReduceSite[] }[],
  allow: Record<string, string>
): { unallowlisted: string[]; stale: string[] } {
  const unallowlisted: string[] = []
  const seen = new Set<string>()

  for (const { file, content, sites } of filesToSites) {
    for (const site of sites) {
      if (isReceiverSafe(site.receiver, content)) continue
      const allowKey = `${file}::${site.key}`
      seen.add(allowKey)
      if (!(allowKey in allow)) {
        unallowlisted.push(`${file}: ${site.key}`)
      }
    }
  }

  const stale = Object.keys(allow).filter((k) => !seen.has(k))

  unallowlisted.sort()
  stale.sort()
  return { unallowlisted, stale }
}

// ---- allowedRawSubAllocationReduces -----------------------------------------------------------
// Empty today: every production `.reduce()` over allocated_amount/spent_amount in this feature
// already reduces over filterTopLevel's output (directly or via a `topLevel` local). Keyed by
// "<path under features/budgets/>::<normalized receiver.reduce(...) snippet>" — not by line
// number, which rots — with a reason for why the exemption is safe despite not routing through
// filterTopLevel. Add an entry here, with a reason, in the same commit that adds such a site.
const allowedRawSubAllocationReduces: Record<string, string> = {}

// ---- The live guard. -----------------------------------------------------------------------

function collectFiles(dir: string): string[] {
  const files: string[] = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    const stat = statSync(full)
    if (stat.isDirectory()) {
      files.push(...collectFiles(full))
    } else if (
      (entry.endsWith('.ts') || entry.endsWith('.tsx')) &&
      !entry.endsWith('.test.ts') &&
      !entry.endsWith('.test.tsx')
    ) {
      files.push(full)
    }
  }
  return files
}

function scanBudgetsFeatureForRawSubAllocationReduces(): {
  unallowlisted: string[]
  stale: string[]
} {
  const filesToSites = collectFiles(BUDGETS_ROOT).map((absPath) => {
    const content = readFileSync(absPath, 'utf-8')
    const relPath = relative(BUDGETS_ROOT, absPath)
    return { file: relPath, content, sites: reduceSitesIn(content) }
  })
  return subAllocationReduceVerdict(filesToSites, allowedRawSubAllocationReduces)
}

describe('sub-allocation total guard (frontend half, Issue #1630)', () => {
  it('flags no .reduce() over allocated_amount/spent_amount that skips filterTopLevel, and no stale allowlist entry', () => {
    const { unallowlisted, stale } = scanBudgetsFeatureForRawSubAllocationReduces()
    expect(
      unallowlisted,
      `.reduce() over allocated_amount/spent_amount with no filterTopLevel and no allowlist entry:\n` +
        unallowlisted.map((u) => `  ${u}`).join('\n') +
        '\nEither reduce over filterTopLevel(categories) (or a local assigned from it), or add a ' +
        'reasoned entry to allowedRawSubAllocationReduces.'
    ).toHaveLength(0)
    expect(
      stale,
      `allowedRawSubAllocationReduces entries with no matching site (allowlist rot):\n` +
        stale.map((s) => `  ${s}`).join('\n')
    ).toHaveLength(0)
  })

  it('every allowlist entry carries a reason', () => {
    for (const [key, reason] of Object.entries(allowedRawSubAllocationReduces)) {
      expect(reason.trim().length, `allowedRawSubAllocationReduces[${key}] has no reason`).toBeGreaterThan(0)
    }
  })
})

// ---- Meta-tests: the constructed violations. --------------------------------------------------

describe('sub-allocation reduce guard meta-tests', () => {
  it('flags a raw .reduce() over allocated_amount with no filterTopLevel anywhere in the file', () => {
    const content = `
      export function computeTotal(categories) {
        return categories.reduce((sum, cat) => sum + cat.allocated_amount, 0)
      }
    `
    const sites = reduceSitesIn(content)
    const { unallowlisted, stale } = subAllocationReduceVerdict(
      [{ file: 'fake.ts', content, sites }],
      {}
    )
    expect(unallowlisted).toHaveLength(1)
    expect(unallowlisted[0]).toContain('fake.ts')
    expect(unallowlisted[0]).toContain('categories.reduce')
    expect(stale).toHaveLength(0)
  })

  it('flags a raw .reduce() over spent_amount with no filterTopLevel anywhere in the file', () => {
    const content = `
      const totalSpent = categories.reduce((sum, cat) => sum + cat.spent_amount, 0)
    `
    const sites = reduceSitesIn(content)
    const { unallowlisted } = subAllocationReduceVerdict([{ file: 'fake.ts', content, sites }], {})
    expect(unallowlisted).toHaveLength(1)
  })

  it('does not flag a .reduce() called directly on filterTopLevel(...)', () => {
    const content = `
      const total = filterTopLevel(categories).reduce((sum, cat) => sum + cat.allocated_amount, 0)
    `
    const sites = reduceSitesIn(content)
    const { unallowlisted, stale } = subAllocationReduceVerdict(
      [{ file: 'fake.ts', content, sites }],
      {}
    )
    expect(unallowlisted).toHaveLength(0)
    expect(stale).toHaveLength(0)
  })

  it('does not flag a .reduce() on a local variable assigned from filterTopLevel(...) earlier in the file', () => {
    const content = `
      const topLevel = filterTopLevel(categories)
      const total = topLevel.reduce((sum, cat) => sum + cat.allocated_amount, 0)
    `
    const sites = reduceSitesIn(content)
    const { unallowlisted } = subAllocationReduceVerdict([{ file: 'fake.ts', content, sites }], {})
    expect(unallowlisted).toHaveLength(0)
  })

  it('still flags a .reduce() on a variable that was never assigned from filterTopLevel', () => {
    const content = `
      const topLevel = categories // NOT filterTopLevel — the exact mutation this guard exists for
      const total = topLevel.reduce((sum, cat) => sum + cat.allocated_amount, 0)
    `
    const sites = reduceSitesIn(content)
    const { unallowlisted } = subAllocationReduceVerdict([{ file: 'fake.ts', content, sites }], {})
    expect(unallowlisted).toHaveLength(1)
  })

  it('flags a stale allowlist entry whose site no longer exists', () => {
    const { unallowlisted, stale } = subAllocationReduceVerdict(
      [{ file: 'fake.ts', content: '', sites: [] }],
      { 'gone.ts::categories.reduce((sum, cat) => sum + cat.allocated_amount, 0)': 'no longer real' }
    )
    expect(unallowlisted).toHaveLength(0)
    expect(stale).toHaveLength(1)
    expect(stale[0]).toContain('gone.ts')
  })

  it('does not flag a .reduce() whose callback never mentions allocated_amount or spent_amount', () => {
    const content = `
      const count = categories.reduce((sum, cat) => sum + 1, 0)
    `
    const sites = reduceSitesIn(content)
    expect(sites).toHaveLength(0)
  })

  it('a site with a matching allowlist entry is reported neither way', () => {
    const content = `
      const total = categories.reduce((sum, cat) => sum + cat.allocated_amount, 0)
    `
    const sites = reduceSitesIn(content)
    expect(sites).toHaveLength(1)
    const allow = { [`fake.ts::${sites[0].key}`]: 'constructed example: this key IS the site above' }
    const { unallowlisted, stale } = subAllocationReduceVerdict(
      [{ file: 'fake.ts', content, sites }],
      allow
    )
    expect(unallowlisted).toHaveLength(0)
    expect(stale).toHaveLength(0)
  })
})
