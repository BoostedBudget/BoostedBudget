/**
 * Client-naming consistency check (Issue #1666, Wave 5 → Todo 16).
 *
 * The public MCP surfaces name the clients BoostedBudget supports. Those names
 * drifted historically (`Claude` / `Claude Desktop`, `Cursor` / `Cursor & other
 * clients`, a `GitHub Copilot Chat` note), so this file is the token-level guard
 * that keeps the vocabulary consistent across every surface that names a client:
 *
 *   - apps/web/src/features/landing/components/McpBand.tsx  (read-only — already correct)
 *   - apps/web/src/features/mcp/McpPage.tsx
 *   - apps/web/src/features/mcp/components/McpSetupTabs.tsx
 *   - apps/web/src/features/pricing/PricingPage.tsx
 *   - apps/mcp-server/README.md
 *   - apps/web/src/features/features/FeaturesPage.tsx  (created by #1666)
 *   - apps/web/public/llms-full.txt  (created by #1666 — generated from marketingContent.ts)
 *
 * It asserts a *declared vocabulary*, not byte-identical sentences: the surfaces
 * are structurally different (prose lists vs a tab-label array vs a README
 * paragraph), and legitimate aliases (`Claude Code CLI`) and a documented
 * carve-out (`GitHub Copilot Chat`) mean a naive set-membership check can never
 * pass. See the plan's pinned "Definition" / "MATCHING SEMANTICS".
 *
 * ⚠ MATCHING SEMANTICS — all client / denylist matching is WHOLE-WORD **and**
 * CASE-SENSITIVE. This is load-bearing, not stylistic: `Zed` is a substring of
 * `dramatized` (McpPage.tsx), `miscategorized` (McpPage.tsx) and `recategorized`
 * (apps/mcp-server/README.md); `Continue` matches the lowercase English word
 * `continue` (README.md). A substring or case-insensitive matcher false-fails on
 * files that are already correct. `occurrences()` below implements it, and the
 * "matcher semantics" suite pins it independent of the files.
 */

import { describe, it, expect } from 'bun:test'
import { readFileSync } from 'node:fs'

/** Canonical clients the product actually claims. */
const CANONICAL_CLIENTS = ['Claude Desktop', 'Claude Code', 'Cursor', 'VS Code'] as const

/**
 * Declared aliases, each of which resolves to one or more canonical clients.
 * An alias is not a client of its own — it is an alternate way of naming a
 * canonical one.
 */
const ALIASES: Record<string, readonly string[]> = {
  Claude: ['Claude Desktop'],
  'Claude Code CLI': ['Claude Code'],
  'Claude Code extension for VS Code': ['Claude Code', 'VS Code'],
  'Cursor & other clients': ['Cursor']
}

/** Generic phrases — not client names, allowed anywhere. */
const GENERIC_PHRASES = ['any MCP-compatible client', 'any MCP-compatible agent', 'other clients'] as const

/**
 * Clients the product does NOT claim. Their presence anywhere is a failure.
 * This is also the check's "names outside the vocabulary" detector: every one of
 * these is a recognised client mention that is deliberately not in the allowed set.
 */
const DENYLIST = [
  'ChatGPT',
  'Gemini',
  'Zed',
  'Windsurf',
  'Cline',
  'Continue',
  'JetBrains',
  'Neovim'
] as const

/**
 * Documented carve-out. `GitHub Copilot Chat` is a fixture of VS Code's built-in
 * MCP support — a feature note, not a supported-client claim — and must appear
 * ONLY inside McpSetupTabs.tsx (and, stronger, only inside its VsCodePanel).
 */
const CARVE_OUT = 'GitHub Copilot Chat'

/** Every client-like token the check knows how to recognise. */
const RECOGNISED_MENTIONS = [
  ...CANONICAL_CLIENTS,
  ...Object.keys(ALIASES),
  ...DENYLIST,
  CARVE_OUT
]

const SURFACES: Record<string, string> = {
  'McpBand.tsx': '../landing/components/McpBand.tsx',
  'McpPage.tsx': './McpPage.tsx',
  'McpSetupTabs.tsx': './components/McpSetupTabs.tsx',
  'PricingPage.tsx': '../pricing/PricingPage.tsx',
  'apps/mcp-server/README.md': '../../../../mcp-server/README.md',
  'FeaturesPage.tsx': '../features/FeaturesPage.tsx',
  'apps/web/public/llms-full.txt': '../../../public/llms-full.txt'
}

const SURFACE_TEXT: Record<string, string> = Object.fromEntries(
  Object.entries(SURFACES).map(([name, relativePath]) => [
    name,
    readFileSync(new URL(relativePath, import.meta.url), 'utf8')
  ])
)

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * Count whole-word, case-sensitive occurrences of `needle` in `haystack`.
 * Word boundaries are expressed as `[A-Za-z0-9_]` lookarounds rather than `\b`
 * so a multi-word phrase (`GitHub Copilot Chat`) is anchored only at its outer
 * edges, and so the semantics do not vary with Unicode.
 */
function occurrences(haystack: string, needle: string): number {
  const pattern = new RegExp(`(?<![A-Za-z0-9_])${escapeRegExp(needle)}(?![A-Za-z0-9_])`, 'g')
  return (haystack.match(pattern) ?? []).length
}

function mentionsIn(text: string): string[] {
  return RECOGNISED_MENTIONS.filter((mention) => occurrences(text, mention) > 0)
}

describe('client naming — matcher semantics (regression proof)', () => {
  // These four are the false-fails the pinned semantics exist to prevent. If the
  // matcher regresses to substring or case-insensitive, these go red first —
  // before it can wrongly fail the untouched surfaces below.
  it('does not match `Zed` inside ordinary lowercase words', () => {
    expect(occurrences('a dramatized transcript', 'Zed')).toBe(0)
    expect(occurrences('flags anything it thinks looks miscategorized', 'Zed')).toBe(0)
    expect(occurrences('Netflix recategorized', 'Zed')).toBe(0)
  })

  it('does not match `Continue` in the lowercase English word', () => {
    expect(occurrences('next_cursor to continue', 'Continue')).toBe(0)
  })

  it('is case-sensitive', () => {
    expect(occurrences('chatgpt and gemini', 'ChatGPT')).toBe(0)
    expect(occurrences('chatgpt and gemini', 'Gemini')).toBe(0)
    expect(occurrences('ZED editor', 'Zed')).toBe(0)
  })

  it('is whole-word', () => {
    expect(occurrences('ChatGPTs are many', 'ChatGPT')).toBe(0)
    expect(occurrences('Gemini2 released', 'Gemini')).toBe(0)
  })

  it('still matches a real standalone, correctly-cased mention', () => {
    expect(occurrences('Open ChatGPT now', 'ChatGPT')).toBe(1)
    expect(occurrences('Zed is an editor', 'Zed')).toBe(1)
    expect(occurrences('Click Continue to proceed', 'Continue')).toBe(1)
    expect(occurrences('Use VS Code for this', 'VS Code')).toBe(1)
  })

  it('does not false-fail on the ordinary words the live surfaces actually contain', () => {
    // The false-fails are NOT hypothetical: the scanned files carry these words
    // today, so a matcher regression would turn the denylist suite red on files
    // that are already correct. Pin the real files, not just synthetic strings.
    expect(occurrences(SURFACE_TEXT['McpPage.tsx'], 'Zed')).toBe(0)
    expect(occurrences(SURFACE_TEXT['apps/mcp-server/README.md'], 'Zed')).toBe(0)
    expect(occurrences(SURFACE_TEXT['apps/mcp-server/README.md'], 'Continue')).toBe(0)
    expect(occurrences(SURFACE_TEXT['PricingPage.tsx'], 'Continue')).toBe(0)
    // ...and the words really are in those files, so the check above is not vacuous.
    expect(SURFACE_TEXT['McpPage.tsx']).toMatch(/dramatized/)
    expect(SURFACE_TEXT['McpPage.tsx']).toMatch(/miscategorized/)
    expect(SURFACE_TEXT['apps/mcp-server/README.md']).toMatch(/recategorized/)
    expect(SURFACE_TEXT['apps/mcp-server/README.md']).toMatch(/continue/)
  })
})

describe('client naming — declared vocabulary', () => {
  it('every alias resolves to canonical clients only', () => {
    for (const [alias, targets] of Object.entries(ALIASES)) {
      expect(alias.length).toBeGreaterThan(0)
      for (const target of targets) {
        expect(CANONICAL_CLIENTS).toContain(target as (typeof CANONICAL_CLIENTS)[number])
      }
    }
  })

  it('the denylist names no canonical client or alias', () => {
    for (const denied of DENYLIST) {
      expect(CANONICAL_CLIENTS as readonly string[]).not.toContain(denied)
      expect(Object.keys(ALIASES)).not.toContain(denied)
    }
  })

  it('generic phrases are not themselves client names', () => {
    for (const phrase of GENERIC_PHRASES) {
      expect(CANONICAL_CLIENTS as readonly string[]).not.toContain(phrase)
      expect(Object.keys(ALIASES)).not.toContain(phrase)
      expect(DENYLIST as readonly string[]).not.toContain(phrase)
    }
  })
})

describe('client naming — denylist appears in no surface (regression proof)', () => {
  // (ii) + the matching-semantics regression: this passes on the UNMODIFIED
  // surfaces. A check that is red on untouched input is a broken check.
  for (const [surface, text] of Object.entries(SURFACE_TEXT)) {
    it(`${surface} names none of the denylisted clients`, () => {
      const found = DENYLIST.filter((client) => occurrences(text, client) > 0)
      expect(found).toEqual([])
    })
  }
})

describe('client naming — no surface names a client outside the vocabulary (i)', () => {
  for (const [surface, text] of Object.entries(SURFACE_TEXT)) {
    it(`${surface} only names declared clients`, () => {
      const allowed = new Set<string>([
        ...CANONICAL_CLIENTS,
        ...Object.keys(ALIASES),
        ...(surface === 'McpSetupTabs.tsx' ? [CARVE_OUT] : [])
      ])
      const offending = mentionsIn(text).filter((mention) => !allowed.has(mention))
      expect(offending).toEqual([])
    })
  }
})

describe('client naming — GitHub Copilot Chat carve-out (iii)', () => {
  it('appears only inside McpSetupTabs.tsx', () => {
    const surfacesMentioning = Object.entries(SURFACE_TEXT)
      .filter(([, text]) => occurrences(text, CARVE_OUT) > 0)
      .map(([surface]) => surface)
    expect(surfacesMentioning).toEqual(['McpSetupTabs.tsx'])
  })

  it('is confined to the VS Code panel within McpSetupTabs.tsx', () => {
    const text = SURFACE_TEXT['McpSetupTabs.tsx']
    const panelStart = text.indexOf('function VsCodePanel')
    expect(panelStart).toBeGreaterThan(-1)
    // The next top-level function after VsCodePanel starts the export component.
    const panelEnd = text.indexOf('export function McpSetupTabs', panelStart)
    expect(panelEnd).toBeGreaterThan(panelStart)
    const inVsCodePanel = text.slice(panelStart, panelEnd)
    expect(occurrences(inVsCodePanel, CARVE_OUT)).toBeGreaterThan(0)
    expect(occurrences(text, CARVE_OUT)).toBe(occurrences(inVsCodePanel, CARVE_OUT))
  })
})

describe('client naming — /mcp states the full canonical set (iv)', () => {
  it('McpPage.tsx names all four canonical clients and the generic phrase', () => {
    const text = SURFACE_TEXT['McpPage.tsx']
    const missing = CANONICAL_CLIENTS.filter((client) => occurrences(text, client) === 0)
    expect(missing).toEqual([])
    expect(occurrences(text, 'any other MCP-compatible client')).toBeGreaterThan(0)
  })
})
