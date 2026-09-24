/**
 * Generator for `apps/web/public/llms-full.txt` (Issue #1666, Todo 14).
 *
 * `llms-full.txt` is the full-text companion to the hand-written index at
 * `apps/web/public/llms.txt`: the same document with the landing page's marketing
 * prose — every feature description and every FAQ answer — expanded inline, so an
 * agent can read the content without executing the SPA's JavaScript.
 *
 * It is GENERATED, never hand-written (the plan's Scope OUT lists a hand-authored
 * version as forbidden). The composition is a pure function in
 * `src/features/landing/data/llmsFull.ts`, shared with the drift test so the test
 * and this CLI cannot diverge; this script is the thin write step. The prose comes
 * from `marketingContent.ts`, which carries a stable `key` instead of an icon
 * component, so importing it never pulls `lucide-react` into this Node-side script.
 *
 * Determinism: fixed `\n` line endings and exactly ONE trailing newline.
 * `marketingContent.drift.test.ts` recomputes this in memory and asserts
 * byte-equality with the committed file — and `.gitattributes` pins the file to
 * `eol=lf` so a Windows checkout cannot rewrite it to CRLF and fail that check for
 * a reason unrelated to drift.
 *
 * Run: bun run scripts/generate-llms-full.ts (from apps/web)
 */

import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildLlmsFull } from '../src/features/landing/data/llmsFull'
import { MARKETING_FAQ, MARKETING_FEATURES } from '../src/features/landing/data/marketingContent'

const __dirname = dirname(fileURLToPath(import.meta.url))
const LLMS_TXT_PATH = resolve(__dirname, '../public/llms.txt')
const OUTPUT_PATH = resolve(__dirname, '../public/llms-full.txt')

if (import.meta.main) {
  const output = buildLlmsFull(readFileSync(LLMS_TXT_PATH, 'utf-8'))
  writeFileSync(OUTPUT_PATH, output)
  console.log(
    `✅ llms-full.txt generated: ${output.length} chars ` +
      `(${MARKETING_FEATURES.length} features, ${MARKETING_FAQ.length} FAQ entries)`
  )
}