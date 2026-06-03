/**
 * Anti-bypass CI check (Codex recommendation).
 *
 * Scans apps/mcp-server/src/ for direct fetch/axios calls to the BoostedBudget
 * API that bypass the host-proxy. Every backend call must go through:
 *   host-proxy.ts → createHostProxy() → invoke() → fetch mutator
 *
 * This ensures the manifest gate, credential injection, confirmation tokens,
 * rate limits, and error redaction are always applied.
 *
 * Run via: bun run scripts/check-no-direct-api-calls.ts
 */

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const SRC_DIR = resolve(__dirname, '../src')

// Patterns that indicate direct API calls bypassing the host-proxy
const SUSPICIOUS_PATTERNS = [
  /fetch\s*\(\s*['"](https?:\/\/)?api\./,              // fetch('https://api.boostedbudget.com/...')
  /fetch\s*\(\s*['"](https?:\/\/)?staging-api\./,     // fetch('https://staging-api.boostedbudget.com/...')
  /fetch\s*\(\s*[`'"]\$\{.*(?:API_URL|api)\}/,        // fetch(`${env.BOOSTEDBUDGET_API_URL}/...`)
  /axios\s*\.\s*(?:get|post|put|patch|delete)\s*\(/,  // axios.get/post/etc
  /AXIOS_INSTANCE\s*\(/,                               // direct AXIOS_INSTANCE usage
]

// Files allowed to contain these patterns (the host-proxy is the trusted boundary)
const ALLOWLISTED_FILES = new Set([
  'sandbox/host-proxy.ts',
  'client/fetch-mutator.ts',
  'client/fetch-mutator.test.ts',
  'sandbox/host-proxy.test.ts',
])

function scanDir(dir: string, root: string): string[] {
  const violations: string[] = []
  for (const entry of readdirSync(dir)) {
    const fullPath = join(dir, entry)
    const relPath = fullPath.replace(root + '/', '')
    const stat = statSync(fullPath)

    if (stat.isDirectory()) {
      if (entry !== 'node_modules' && entry !== 'generated') {
        violations.push(...scanDir(fullPath, root))
      }
      continue
    }

    if (!entry.endsWith('.ts')) continue
    if (ALLOWLISTED_FILES.has(relPath)) continue

    const content = readFileSync(fullPath, 'utf-8')
    for (const pattern of SUSPICIOUS_PATTERNS) {
      if (pattern.test(content)) {
        violations.push(`${relPath}: potential direct API call (pattern: ${pattern.source})`)
        break
      }
    }
  }
  return violations
}

const violations = scanDir(SRC_DIR, SRC_DIR)

if (violations.length > 0) {
  console.error('❌ Direct API call check FAILED — bypass detected:')
  for (const v of violations) console.error(`   ${v}`)
  console.error('\nAll API calls must go through the host-proxy (sandbox/host-proxy.ts).')
  process.exit(1)
}

console.log('✅ No direct API call bypasses detected')
