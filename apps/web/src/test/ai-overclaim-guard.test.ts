/**
 * AI-overclaim guard (Issue #1411)
 *
 * Earlier tasks in #1411 removed AI overclaims ("AI-powered insights" and friends) and
 * stale "2-month" trial wording from the landing page, pricing page, billing/profile
 * surfaces, the Terms of Service, and index.html — standardizing trial copy on "60-day"
 * (config/subscription_tiers.yaml: trial.duration_days: 60). Those edits were one-off;
 * without a guard, the next developer re-types "AI-powered insights" into marketing copy
 * in a month and nobody notices. This test makes that decision permanent.
 *
 * Modeled on apps/web/src/test/raw-palette-guard.test.ts: walk directories, read each
 * source file, fail on a regex match, name the file and line.
 *
 * IMPORTANT: this file itself necessarily contains the banned strings (they appear in the
 * regex literals below), and legitimate component tests may assert a banned string is
 * *absent* (containing the string as part of that assertion). The scanner therefore
 * excludes test/spec files, everything under src/test/, and anything under a fixtures/ or
 * __tests__/ directory — production sources only.
 *
 * features/mcp is deliberately NOT scanned: the /mcp page is the honest home for the AI
 * agent narrative (BoostedBudget's MCP server lets AI agents read/act on your finances).
 * Do not add it here.
 */
import { describe, it, expect } from 'bun:test'
import { readdirSync, readFileSync, statSync } from 'fs'
import { join, relative } from 'path'

// import.meta.dir === apps/web/src/test — walk up to apps/web/src, apps/web, and repo root
const SRC_ROOT = join(import.meta.dir, '..')
const WEB_ROOT = join(SRC_ROOT, '..')
const REPO_ROOT = join(WEB_ROOT, '..', '..')

const SCAN_DIRS = ['features/landing', 'features/pricing', 'features/profile', 'features/billing', 'legal'].map(d =>
  join(SRC_ROOT, d)
)
const INDEX_HTML = join(WEB_ROOT, 'index.html')

// Pattern 1: claims that the product's OWN automation/system is intelligent or learning.
// Deliberately does NOT ban the bare token "AI" — features/mcp legitimately discusses AI
// agents connecting to BoostedBudget, and is out of scope for this scan entirely.
const AI_OVERCLAIM_PATTERN =
  /AI[- ]powered|AI insights|artificial intelligence|machine learning|learns from|intelligent (system|automation)|self-learning|predictive/i

// Pattern 2: stale trial wording. Source of truth is config/subscription_tiers.yaml
// (trial.duration_days: 60) — every user-facing surface must say "60-day".
const STALE_TRIAL_PATTERN = /2-month/i

function isExcluded(relPathFromSrc: string): boolean {
  if (/\.(test|spec)\.tsx?$/.test(relPathFromSrc)) return true
  if (relPathFromSrc === 'test' || relPathFromSrc.startsWith('test/') || relPathFromSrc.includes('/test/')) {
    return true
  }
  if (relPathFromSrc.includes('fixtures/')) return true
  if (relPathFromSrc.includes('__tests__/')) return true
  return false
}

function collectFiles(dir: string): string[] {
  const files: string[] = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    const stat = statSync(full)
    if (stat.isDirectory()) {
      files.push(...collectFiles(full))
    } else if (entry.endsWith('.tsx') || entry.endsWith('.ts')) {
      files.push(full)
    }
  }
  return files
}

function scanTargets(): string[] {
  const targets: string[] = []
  for (const scanDir of SCAN_DIRS) {
    for (const filePath of collectFiles(scanDir)) {
      const relFromSrc = relative(SRC_ROOT, filePath)
      if (isExcluded(relFromSrc)) continue
      targets.push(filePath)
    }
  }
  targets.push(INDEX_HTML)
  return targets
}

interface Violation {
  file: string
  line: number
  match: string
}

function scanForPattern(pattern: RegExp): Violation[] {
  const hits: Violation[] = []

  for (const filePath of scanTargets()) {
    const relPath = relative(REPO_ROOT, filePath)
    const contents = readFileSync(filePath, 'utf-8')
    const lines = contents.split('\n')

    for (let i = 0; i < lines.length; i++) {
      const match = lines[i].match(pattern)
      if (match) {
        hits.push({ file: relPath, line: i + 1, match: match[0] })
      }
    }
  }

  return hits
}

function formatReport(hits: Violation[], reason: string): string {
  return hits.map(h => `  ${h.file}:${h.line}: matched "${h.match}" — ${reason}`).join('\n')
}

describe('ai-overclaim guard (#1411)', () => {
  it('has no AI-capability overclaims in landing/pricing/profile/billing/legal or index.html', () => {
    const reason =
      'the product does not claim its own automation is "intelligent" or "learning" — describe the actual rule/automation behavior instead, or move genuine AI-agent copy to features/mcp'
    const hits = scanForPattern(AI_OVERCLAIM_PATTERN)
    expect(hits, `AI overclaim(s) found:\n${formatReport(hits, reason)}`).toHaveLength(0)
  })

  it('has no stale "2-month" trial wording in landing/pricing/profile/billing/legal or index.html', () => {
    const reason = 'the trial is 60 days (config/subscription_tiers.yaml: trial.duration_days: 60) — use "60-day" instead'
    const hits = scanForPattern(STALE_TRIAL_PATTERN)
    expect(hits, `Stale trial wording found:\n${formatReport(hits, reason)}`).toHaveLength(0)
  })
})
