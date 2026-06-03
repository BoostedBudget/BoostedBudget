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
