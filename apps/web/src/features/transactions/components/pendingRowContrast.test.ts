/**
 * Issue #1569 Task 20 (A3) — the pending-transaction row tint failed WCAG AA.
 *
 * `bg-warning/10` (hover `bg-warning/20`) is applied to a table ROW. Rows have no background of
 * their own — DataTable paints `bg-card` — so the tint composites straight onto the card and every
 * cell on the row is read against the tinted result. Measured against the real tokens imported by
 * src/index.css:
 *
 *   light   muted-foreground on card            4.73:1   (already thin)
 *           on card + warning@10%               4.38:1   FAIL
 *           on card + warning@20% (hover)       4.05:1   FAIL
 *   dark    on card + warning@10%               5.69:1
 *           on card + warning@20% (hover)       4.44:1   FAIL   (not in the original report)
 *
 * Three of the four states are below the 4.5:1 floor, and the cells that carry
 * `text-muted-foreground` on this table are the DATE column and the category label — content, not
 * decoration. 14px regular is not WCAG "large text", so 3:1 does not apply.
 *
 * ⚠ The sibling defect this must not reproduce: the reversed-row treatment originally used
 * `opacity-70`, which composited the WHOLE row onto the ground and took the amount down to 2.71:1.
 * That was fixed by REMOVING the row-level treatment rather than tuning it, and the same reasoning
 * applies here — tuning the alpha buys at most 4.55:1 at `/5`, and a 0.05 margin against a rounding
 * boundary is not a margin. Task 12 already rejected a 4.57:1 tint on exactly that ground.
 *
 * WHAT THIS TEST DOES, and why it is not a snapshot of a decision. It follows index.css's design
 * token import and reads the pending class string from the components' own source, so it measures
 * what those files actually say. Re-adding any `bg-*` tint that pushes a state below 4.5:1 turns it
 * red, in either theme, whatever the alpha.
 */

import { describe, it, expect } from 'bun:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'

const SRC = join(import.meta.dir, '..', '..', '..')

// ── colour maths ────────────────────────────────────────────────────────────────────────────────

type RGB = [number, number, number]

function oklchToSrgb(L: number, C: number, H: number): RGB {
  const h = (H * Math.PI) / 180
  const a = C * Math.cos(h)
  const b = C * Math.sin(h)
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3
  const lin = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ]
  return lin.map((x) => {
    const c = Math.min(1, Math.max(0, x))
    return c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055
  }) as RGB
}

function relativeLuminance([r, g, b]: RGB): number {
  const lin = (v: number) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

function contrast(fg: RGB, bg: RGB): number {
  const a = relativeLuminance(fg)
  const b = relativeLuminance(bg)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

function composite(fg: RGB, alpha: number, bg: RGB): RGB {
  return fg.map((c, i) => c * alpha + bg[i] * (1 - alpha)) as RGB
}

// ── tokens, read from the real stylesheet ───────────────────────────────────────────────────────

const indexCssPath = join(SRC, 'index.css')
const indexCss = readFileSync(indexCssPath, 'utf8')
const tokenImport = indexCss.match(
  /@import\s+["'](@boostedbudget\/design-tokens\/web\.css)["'];/,
)
if (!tokenImport) {
  throw new Error(`${indexCssPath} must import @boostedbudget/design-tokens/web.css`)
}
const tokenStylesheetPath = createRequire(join(dirname(indexCssPath), 'resolve-token.css')).resolve(
  tokenImport[1],
)
const css = readFileSync(tokenStylesheetPath, 'utf8')

/**
 * `.dark` redefines the same custom properties further down the file, so the LAST occurrence is the
 * dark value and the first is the light one. Both are asserted to exist: a token that silently
 * stopped matching would make every ratio below a measurement of nothing.
 */
function token(name: string, theme: 'light' | 'dark'): RGB {
  const re = new RegExp(`--${name}:\\s*oklch\\(([^)]+)\\)`, 'g')
  const hits = [...css.matchAll(re)]
  expect(
    hits.length,
    `--${name} must be defined for both themes in ${tokenStylesheetPath}`,
  ).toBeGreaterThanOrEqual(2)
  const raw = (theme === 'light' ? hits[0] : hits[hits.length - 1])[1]
  const [L, C, H] = raw.trim().split(/\s+/).map(Number)
  return oklchToSrgb(L, C, H ?? 0)
}

// ── the pending class strings, read from the components' own source ─────────────────────────────

/** Extracts the single-quoted class string guarded by `<flag> &&` in the given source. */
function pendingClasses(file: string, guard: string): string {
  const src = readFileSync(file, 'utf8')
  const i = src.indexOf(guard)
  expect(i, `"${guard}" must appear in ${file} — this test reads the real class list`).toBeGreaterThan(-1)
  const m = /'([^']*)'/.exec(src.slice(i))
  expect(m, `no class string follows "${guard}" in ${file}`).not.toBeNull()
  return m![1]
}

/** Every `bg-<token>/<alpha>` (including `hover:` variants) in a class string. */
function backgroundTints(classes: string): { label: string; token: string; alpha: number }[] {
  return [...classes.matchAll(/(?:([a-z-]+):)?bg-([a-z-]+)\/(\d+)/g)].map((m) => ({
    label: m[1] ? `${m[1]}:` : 'base',
    token: m[2],
    alpha: Number(m[3]) / 100,
  }))
}

const AA_NORMAL_TEXT = 4.5

describe('pending transaction row tint — WCAG AA (#1569 A3)', () => {
  it('follows the design-token stylesheet imported by index.css', () => {
    expect(tokenImport[1]).toBe('@boostedbudget/design-tokens/web.css')
    expect(tokenStylesheetPath).toContain('/packages/design-tokens/generated/web.css')
    expect(css).toContain('--card:')
    expect(css).toContain('.dark {')
  })

  // The positive control. If this drifts, every "PASS" below is a measurement of the wrong maths,
  // and the 4.38 figure this whole task rests on would be unverifiable.
  it('reproduces the reported failing ratios from the real tokens', () => {
    const card = token('card', 'light')
    const warning = token('warning', 'light')
    const muted = token('muted-foreground', 'light')

    expect(contrast(muted, card)).toBeCloseTo(4.73, 1)
    expect(contrast(muted, composite(warning, 0.1, card))).toBeCloseTo(4.38, 1)
    expect(contrast(muted, composite(warning, 0.2, card))).toBeCloseTo(4.05, 1)

    const darkCard = token('card', 'dark')
    const darkWarning = token('warning', 'dark')
    const darkMuted = token('muted-foreground', 'dark')
    // Below the floor too, and NOT in the original report — found by recomputing both themes
    // rather than trusting the light-mode figures.
    expect(contrast(darkMuted, composite(darkWarning, 0.2, darkCard))).toBeCloseTo(4.44, 1)
  })

  const surfaces: { name: string; file: string; guard: string }[] = [
    {
      name: 'TransactionTable row',
      file: join(SRC, 'features/transactions/components/TransactionTable.tsx'),
      guard: 'row.pending &&',
    },
    {
      name: 'TransactionCard',
      file: join(SRC, 'features/transactions/components/TransactionCard.tsx'),
      guard: 'pending &&',
    },
  ]

  for (const surface of surfaces) {
    for (const theme of ['light', 'dark'] as const)
      it(`${surface.name}: muted text clears ${AA_NORMAL_TEXT}:1 in ${theme} in every state`, () => {
        const card = token('card', theme)
        const muted = token('muted-foreground', theme)
        const classes = pendingClasses(surface.file, surface.guard)

        // The untinted baseline must hold, otherwise the palette itself is the problem and no
        // change to these components can fix it.
        expect(contrast(muted, card)).toBeGreaterThanOrEqual(AA_NORMAL_TEXT)

        for (const tint of backgroundTints(classes)) {
          const bg = composite(token(tint.token, theme), tint.alpha, card)
          const ratio = contrast(muted, bg)
          expect(
            ratio,
            `${surface.name} applies ${tint.label}bg-${tint.token}/${tint.alpha * 100} to a surface with ` +
              `no background of its own, so text-muted-foreground is read at ${ratio.toFixed(2)}:1 ` +
              `against the composited result (${theme}). The floor is ${AA_NORMAL_TEXT}:1 for normal ` +
              `text, and the date column and category label on a pending row are exactly that.`
          ).toBeGreaterThanOrEqual(AA_NORMAL_TEXT)
        }
      })
  }

  it('pending is still conveyed by something other than colour', () => {
    for (const surface of surfaces) {
      const src = readFileSync(surface.file, 'utf8')
      expect(src, `${surface.name} must keep a textual Pending marker`).toContain('Pending')
    }
  })
})
