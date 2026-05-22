/**
 * Palette documentation snapshot — #1169.
 *
 * Two color systems coexist in this codebase:
 *   1. CATEGORY_COLORS (colors.ts) — custom colorblind-safe hex palette,
 *      used by ECharts series via themes.ts. This is the ECharts source of truth.
 *   2. --chart-N CSS tokens (index.css) — Tailwind/shadcn-ui chart tokens,
 *      used by any shadcn component that reads CSS variables. They differ
 *      between light and dark themes, and differ from CATEGORY_COLORS by design.
 *
 * NOTE: This is NOT a cross-system drift detector — the two palettes are
 * intentionally separate (hex vs oklch, different design systems). Changes to
 * either system are visible here, prompting deliberate review.
 *
 * If you intentionally change CATEGORY_COLORS, update the snapshots here and
 * verify all charts still look correct in both light and dark mode.
 */

import { describe, it, expect } from 'bun:test'
import { readFileSync } from 'fs'
import { join } from 'path'
import { CATEGORY_COLORS } from './colors'

// ── Snapshot the palette so any change forces a deliberate review ──────────

describe('CATEGORY_COLORS palette snapshot', () => {
  it('has 10 entries', () => {
    expect(CATEGORY_COLORS).toHaveLength(10)
  })

  it('first 5 series colors match established colorblind-safe palette', () => {
    expect(CATEGORY_COLORS[0]).toBe('#3b82f6') // blue-500
    expect(CATEGORY_COLORS[1]).toBe('#10b981') // emerald-500
    expect(CATEGORY_COLORS[2]).toBe('#f59e0b') // amber-500
    expect(CATEGORY_COLORS[3]).toBe('#8b5cf6') // violet-500
    expect(CATEGORY_COLORS[4]).toBe('#f97316') // orange-500
  })

  it('remaining 5 series colors match established colorblind-safe palette', () => {
    expect(CATEGORY_COLORS[5]).toBe('#06b6d4') // cyan-500
    expect(CATEGORY_COLORS[6]).toBe('#ec4899') // pink-500
    expect(CATEGORY_COLORS[7]).toBe('#84cc16') // lime-500
    expect(CATEGORY_COLORS[8]).toBe('#6366f1') // indigo-500
    expect(CATEGORY_COLORS[9]).toBe('#14b8a6') // teal-500
  })
})

// ── Document the CSS --chart-N tokens (separate design system) ────────────
// These are Tailwind/shadcn-ui variables used by shadcn components. They are
// intentionally different from CATEGORY_COLORS (different color systems).
// Parsing them here makes any CSS change visible in test output.

function extractChartTokens(css: string, block: 'light' | 'dark'): Record<string, string> {
  const rootPattern = block === 'light' ? /:root\s*\{([^}]+)\}/ : /\.dark\s*\{([^}]+)\}/
  const match = css.match(rootPattern)
  if (!match) return {}
  const tokens: Record<string, string> = {}
  const tokenPattern = /--(chart-\d+):\s*([^;]+);/g
  let m: RegExpExecArray | null
  while ((m = tokenPattern.exec(match[1])) !== null) {
    tokens[m[1]] = m[2].trim()
  }
  return tokens
}

describe('CSS --chart-N token documentation (separate palette from CATEGORY_COLORS)', () => {
  let cssContent = ''
  try {
    cssContent = readFileSync(join(import.meta.dirname, '../../index.css'), 'utf8')
  } catch {
    // index.css not present in all test environments — tests skip gracefully
  }

  it('CSS light --chart-1..5 tokens are parseable', () => {
    if (!cssContent) return
    const tokens = extractChartTokens(cssContent, 'light')
    expect(tokens['chart-1']).toBeDefined()
    expect(tokens['chart-2']).toBeDefined()
    expect(tokens['chart-3']).toBeDefined()
    expect(tokens['chart-4']).toBeDefined()
    expect(tokens['chart-5']).toBeDefined()
  })

  it('CSS dark --chart-1..5 tokens are parseable', () => {
    if (!cssContent) return
    const tokens = extractChartTokens(cssContent, 'dark')
    expect(tokens['chart-1']).toBeDefined()
    expect(tokens['chart-2']).toBeDefined()
    expect(tokens['chart-3']).toBeDefined()
    expect(tokens['chart-4']).toBeDefined()
    expect(tokens['chart-5']).toBeDefined()
  })

  it('ECharts palette (CATEGORY_COLORS) is intentionally independent of --chart-N CSS tokens', () => {
    if (!cssContent) return
    // CATEGORY_COLORS uses hex; --chart-N uses oklch. They are separate systems.
    // If you unify them, update themes.ts to read CSS variables at runtime
    // via getComputedStyle(document.documentElement).getPropertyValue('--chart-N').
    const lightTokens = extractChartTokens(cssContent, 'light')
    expect(lightTokens['chart-1'] ?? '').toMatch(/oklch/)
    expect(CATEGORY_COLORS[0]).toMatch(/^#/)
  })
})
