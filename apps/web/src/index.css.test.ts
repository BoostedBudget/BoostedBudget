import { describe, it, expect } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'index.css'), 'utf8')

// Guard against re-introducing the unlayered universal reset that breaks
// Tailwind v4 utility padding everywhere (the root cause from issue #1172).
//
// Tailwind v4 emits utilities inside @layer utilities; per the CSS Cascade Layers
// spec, an unlayered rule beats any layered rule — including utilities.
// So `* { padding: 0 }` outside a @layer would silently nuke .px-*, .py-*, .p-*.
describe('index.css — cascade-layer regression guard', () => {
  it('has no unlayered universal padding reset', () => {
    // Remove @layer blocks to isolate top-level (unlayered) CSS.
    let depth = 0
    let insideLayer = false
    let topLevel = ''
    for (let i = 0; i < css.length; i++) {
      const ch = css[i]
      if (ch === '{') {
        if (depth === 0) {
          const before = css.slice(0, i).trimEnd()
          const layerIdx = before.lastIndexOf('@layer')
          if (layerIdx !== -1) {
            const afterLayer = before.slice(layerIdx)
            if (!afterLayer.includes('{')) {
              insideLayer = true
            }
          }
        }
        depth++
      } else if (ch === '}') {
        depth--
        if (depth === 0) insideLayer = false
      } else if (depth === 0 && !insideLayer) {
        topLevel += ch
      }
    }

    const unlayeredUniversalPaddingReset = /\*\s*\{[^}]*\bpadding\s*:/
    expect(topLevel).not.toMatch(unlayeredUniversalPaddingReset)
  })

  it('box-sizing:border-box appears inside @layer base', () => {
    const layerBaseMatch = /@layer\s+base\s*\{([\s\S]*?)\}\s*(?=@|$)/g
    let found = false
    let match: RegExpExecArray | null
    while ((match = layerBaseMatch.exec(css)) !== null) {
      if (match[1].includes('box-sizing') && match[1].includes('border-box')) {
        found = true
        break
      }
    }
    expect(found).toBe(true)
  })

  it('html[data-resizing] cursor rule is present', () => {
    expect(css).toMatch(/html\[data-resizing\]/)
    expect(css).toMatch(/col-resize/)
  })
})

// #1631: guard the global prefers-reduced-motion coverage for animate-spin/
// animate-pulse. A weak version of this test would just grep for the media
// query string — assert the specific utility selectors are actually declared
// inside it, and that it stays unlayered (a @layer wrapper would lose to
// Tailwind's own layered utilities per the Cascade Layers spec).
describe('index.css — reduced-motion spin/pulse coverage', () => {
  const reducedMotionBlocks = [
    ...css.matchAll(/@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{([\s\S]*?)\n\}/g),
  ].map((match) => match[1])

  it('has at least one prefers-reduced-motion: reduce block', () => {
    expect(reducedMotionBlocks.length).toBeGreaterThan(0)
  })

  it('covers .animate-spin with a non-zero animation-duration', () => {
    const block = reducedMotionBlocks.find((b) => /\.animate-spin\s*\{/.test(b))
    expect(block).toBeDefined()
    const rule = block!.match(/\.animate-spin\s*\{([^}]*)\}/)
    expect(rule).not.toBeNull()
    expect(rule![1]).toMatch(/animation-duration\s*:\s*[1-9]/)
    // Must not freeze the spinner — a frozen spinner reads as a hung page.
    expect(rule![1]).not.toMatch(/animation\s*:\s*none/)
    expect(rule![1]).not.toMatch(/animation-name\s*:\s*none/)
  })

  it('covers .animate-pulse with a non-zero animation-duration', () => {
    const block = reducedMotionBlocks.find((b) => /\.animate-pulse\s*\{/.test(b))
    expect(block).toBeDefined()
    const rule = block!.match(/\.animate-pulse\s*\{([^}]*)\}/)
    expect(rule).not.toBeNull()
    expect(rule![1]).toMatch(/animation-duration\s*:\s*[1-9]/)
  })

  it('the spin/pulse reduced-motion block is unlayered (not inside @layer)', () => {
    // Reuse the same top-level extraction as the padding-reset guard above:
    // strip out anything inside a @layer { ... } block and confirm the
    // reduced-motion media query for animate-spin still shows up outside it.
    let depth = 0
    let insideLayer = false
    let topLevel = ''
    for (let i = 0; i < css.length; i++) {
      const ch = css[i]
      if (ch === '{') {
        if (depth === 0) {
          const before = css.slice(0, i).trimEnd()
          const layerIdx = before.lastIndexOf('@layer')
          if (layerIdx !== -1 && !before.slice(layerIdx).includes('{')) {
            insideLayer = true
          }
        }
        depth++
      } else if (ch === '}') {
        depth--
        if (depth === 0) insideLayer = false
      } else if (depth === 0 && !insideLayer) {
        topLevel += ch
      }
    }

    expect(topLevel).toMatch(/@media\s*\(prefers-reduced-motion:\s*reduce\)/)
    expect(topLevel).toMatch(/\.animate-spin/)
    expect(topLevel).toMatch(/\.animate-pulse/)
  })
})
