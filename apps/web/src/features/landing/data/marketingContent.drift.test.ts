/**
 * Drift test for the generated `apps/web/public/llms-full.txt` (Issue #1666, Todo 14).
 *
 * `llms-full.txt` is a COMMITTED, generated artifact — never hand-written. It is
 * composed by the pure `buildLlmsFull` in `./llmsFull.ts` (the same function the
 * CLI at `scripts/generate-llms-full.ts` writes with) from ONE source of marketing
 * prose (`marketingContent.ts`, shared with the React components) plus the
 * hand-written index at `public/llms.txt`. This test is the tripwire: it recomputes
 * the file in memory and asserts BYTE-EQUALITY with what is committed, so a
 * component/copy edit that forgets to regenerate fails loudly instead of shipping a
 * stale agent-readable file.
 *
 * The assertions deliberately mirror `capability-summary.drift.test.ts` (the
 * repo's generator precedent): a fresh in-memory recompute compared to the
 * committed bytes, plus a length floor so a broken generator that emits nothing
 * cannot pass vacuously, plus a trailing-newline pin.
 *
 * ⚠ Byte-equality is the reason `.gitattributes` pins this file to `eol=lf`: a
 * Windows checkout with `core.autocrlf=true` would otherwise rewrite it to CRLF
 * and fail this test for a reason that is not drift.
 */

import { describe, expect, it } from 'bun:test'
import { Buffer } from 'node:buffer'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildLlmsFull } from './llmsFull'
import { MARKETING_FAQ, MARKETING_FEATURES } from './marketingContent'

const __dirname = dirname(fileURLToPath(import.meta.url))
const LLMS_TXT_PATH = resolve(__dirname, '../../../../public/llms.txt')
const LLMS_FULL_PATH = resolve(__dirname, '../../../../public/llms-full.txt')

const committedBytes = readFileSync(LLMS_FULL_PATH)

describe('llms-full.txt drift guard (#1666)', () => {
  it('is committed and non-trivial in length (a floor, so an empty generator fails loudly)', () => {
    // The composed file is several KB (10 answers + 6 feature descriptions + the
    // index sections). A floor well below that, but far above zero, catches a
    // generator that silently emits nothing or only a header.
    expect(committedBytes.length).toBeGreaterThan(2000)
  })

  it('matches the bytes freshly recomputed from marketingContent.ts + llms.txt', () => {
    const fresh = buildLlmsFull(readFileSync(LLMS_TXT_PATH, 'utf-8'))
    const freshBytes = Buffer.from(fresh, 'utf-8')

    expect(
      freshBytes.equals(committedBytes),
      'apps/web/public/llms-full.txt is stale — regenerate with: ' +
        'bun run scripts/generate-llms-full.ts (from apps/web)'
    ).toBe(true)
  })

  it('is deterministic: two in-memory builds are byte-identical', () => {
    const index = readFileSync(LLMS_TXT_PATH, 'utf-8')
    expect(buildLlmsFull(index)).toBe(buildLlmsFull(index))
  })

  it('uses LF line endings and exactly one trailing newline', () => {
    const text = committedBytes.toString('utf-8')
    expect(text.includes('\r')).toBe(false)
    expect(text.endsWith('\n')).toBe(true)
    expect(text.endsWith('\n\n')).toBe(false)
  })

  it('contains every FAQ question (all 10: 7 landing + 3 agentic)', () => {
    const text = committedBytes.toString('utf-8')
    expect(MARKETING_FAQ.length).toBe(10)
    for (const item of MARKETING_FAQ) {
      expect(text, `llms-full.txt is missing FAQ question: ${item.question}`).toContain(item.question)
    }
  })

  it('contains every feature title (all 6)', () => {
    const text = committedBytes.toString('utf-8')
    expect(MARKETING_FEATURES.length).toBe(6)
    for (const feature of MARKETING_FEATURES) {
      expect(text, `llms-full.txt is missing feature title: ${feature.title}`).toContain(feature.title)
    }
  })
})