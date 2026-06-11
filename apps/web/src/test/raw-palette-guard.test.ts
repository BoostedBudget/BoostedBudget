import { describe, it, expect } from 'bun:test'
import { readdirSync, readFileSync, statSync } from 'fs'
import { join, relative } from 'path'

const ROOT = join(import.meta.dir, '..')  // points to apps/web/src
const SCAN_DIRS = ['features', 'shared', 'auth'].map(d => join(ROOT, d))

const ALLOWLIST_PATHS = [
  'features/landing',
  'features/net-worth/components/NetWorthHistoryChart.tsx',
  'features/dashboard/components/NetWorthSparkline.tsx',
  'features/tags/components/TagEditor.tsx',
  'features/accounts/components/AccountIconDisplay.tsx',
  'features/budgets/components/category-management/AllocationBreakdown.tsx',
]

// TODO: add blue once CTAs/focus-rings/brand colors are triaged (#follow-up)
const CLASS_PATTERN = /\b(bg|text|border|ring)-(green|red|yellow|amber|orange|emerald|rose|gray|slate|zinc)-[0-9]/
const DARK_PATTERN = /dark:(bg|text|border|ring)-(green|red|yellow|amber|orange|emerald|rose|gray|slate|zinc)-/

function isAllowlisted(relPath: string): boolean {
  return ALLOWLIST_PATHS.some(p => relPath.includes(p))
}

function isChartFile(relPath: string): boolean {
  return relPath.endsWith('Chart.tsx') || relPath.endsWith('Sparkline.tsx')
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

function scanForRawPalette(): { file: string; line: number; content: string }[] {
  const hits: { file: string; line: number; content: string }[] = []

  for (const scanDir of SCAN_DIRS) {
    const files = collectFiles(scanDir)

    for (const filePath of files) {
      // Compute relative path from src/ for allowlist matching
      const relPath = relative(ROOT, filePath)

      if (isAllowlisted(relPath)) continue
      if (isChartFile(relPath)) continue

      const contents = readFileSync(filePath, 'utf-8')
      const lines = contents.split('\n')

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        const trimmed = line.trim()

        // Skip comment lines
        if (trimmed.startsWith('//') || trimmed.startsWith('*') || trimmed.startsWith('/*')) continue

        // Skip inline style props (different mechanism — JSX `style={...}` or HTML `style="..."`)
        if (/\bstyle\s*=\s*[{"']/.test(line)) continue

        // Skip CSS variable references
        if (line.includes('var(--')) continue

        if (CLASS_PATTERN.test(line) || DARK_PATTERN.test(line)) {
          hits.push({ file: relPath, line: i + 1, content: line })
        }
      }
    }
  }

  return hits
}

describe('raw-palette guard', () => {
  it('has no raw palette status colors in features/, shared/, or auth/', () => {
    const hits = scanForRawPalette()
    const report = hits.map(h => `  ${h.file}:${h.line}: ${h.content.trim()}`).join('\n')
    expect(hits, `Raw palette classes found:\n${report}`).toHaveLength(0)
  })
})
