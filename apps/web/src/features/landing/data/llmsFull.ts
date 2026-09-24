/**
 * Pure renderer for `apps/web/public/llms-full.txt` (Issue #1666, Todo 14).
 *
 * Lives under `src/` — beside `marketingContent.ts` — rather than in the CLI at
 * `scripts/`, because `tsconfig.test.json` lists only `src/` and the drift test
 * must import this function directly to recompute the file in memory. Keeping the
 * renderer pure and separate from the CLI means the test and the generator share
 * ONE implementation and cannot diverge.
 *
 * It consumes the single source of marketing prose (`marketingContent.ts`) and the
 * hand-written index (`public/llms.txt`), whose header and `## Public Pages` /
 * `## Legal` / `## Optional` sections are re-emitted verbatim so the two files can
 * never disagree on section titles or links. `## Features` and `## FAQ` are
 * inserted before `## Public Pages`.
 */

import { MARKETING_FAQ, MARKETING_FEATURES } from './marketingContent'

/** The `## Public Pages` heading is where the full-text body is inserted. */
const INSERT_BEFORE = '## Public Pages'

/**
 * Renders the full-text document from the index text and the marketing prose.
 *
 * Deterministic: fixed `\n` line endings, no trailing whitespace, and exactly ONE
 * trailing newline. The drift test calls this with the committed `llms.txt`
 * contents and asserts byte-equality with the committed `llms-full.txt`.
 */
export function buildLlmsFull(llmsIndexText: string): string {
  const insertAt = llmsIndexText.indexOf(INSERT_BEFORE)
  if (insertAt === -1) {
    throw new Error(
      `llms.txt no longer contains "${INSERT_BEFORE}" — the generator has nowhere to ` +
        'insert the full-text sections. Update the renderer alongside the index.'
    )
  }

  const header = llmsIndexText.slice(0, insertAt).trimEnd()
  const indexSections = llmsIndexText.slice(insertAt).trim()

  const features = MARKETING_FEATURES.map(
    (feature) => `### ${feature.title}\n\n${feature.description}`
  ).join('\n\n')

  const faq = MARKETING_FAQ.map((item) => `### ${item.question}\n\n${item.answer}`).join('\n\n')

  return `${header}\n\n## Features\n\n${features}\n\n## FAQ\n\n${faq}\n\n${indexSections}\n`
}
