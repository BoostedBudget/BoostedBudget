/**
 * MCP `search` tool implementation.
 *
 * Lets agents discover BoostedBudget API operations by keyword, tag, or class.
 * Returns only allowed (non-excluded) operations from the spec index.
 *
 * This tool is read-only and requires no credentials — it operates on the
 * statically-built spec index that is loaded at Worker startup.
 */

import { z } from 'zod'
import { getCapability } from '../../manifest/capability-manifest'
import { searchSpec, listSpecEntries, responseFields } from '../../spec/search-runtime'
import type { SpecEntry, SpecIndex, SpecResponseShape } from '../../spec/search-runtime'
import { isReadExecuteAdmitted } from './read-execute-admission'
import type { ResponseDetail } from '../../responses/projection'

// ── Input schema ────────────────────────────────────────────────────────────

const SearchInputSchema = z.object({
  query: z.string().default(''),
  detail: z.enum(['compact', 'full']).default('compact'),
  // #1569 Task 16: 'destructive' is the third capability class — operations that permanently
  // destroy stored data. Agents must be able to filter FOR it (to ask before calling) and
  // AGAINST it, so it belongs in the discoverable enum.
  class: z.enum(['read', 'write', 'destructive']).optional(),
  tag: z.string().optional(),
  limit: z.number().int().min(1).default(20).transform((v) => Math.min(v, 50)),
})

export type SearchInput = z.infer<typeof SearchInputSchema>

/**
 * Parse and validate search tool input.
 * Throws ZodError on invalid input.
 */
export function parseSearchInput(raw: unknown): SearchInput {
  return SearchInputSchema.parse(raw)
}

// ── Output formatting ────────────────────────────────────────────────────────

export interface McpContent {
  type: 'text'
  text: string
}

// Type alias (vs interface) to satisfy SDK's CallToolResult index signature.
// MCP SDK types CallToolResult with [key: string]: unknown; only a type alias
// with explicit index sig is assignable to that shape.
export type McpToolResult = {
  content: McpContent[]
  [key: string]: unknown
}

function renderResponseShape(shape: SpecResponseShape): string {
  let rendered: string
  if (shape.type === 'array') {
    rendered = `array<${renderResponseShape(shape.items)}>`
  } else {
    rendered = shape.type
  }
  return shape.nullable ? `${rendered} | null` : rendered
}

/**
 * Format search results into an MCP text content block.
 * Excluded operations are filtered out before rendering.
 */
export function buildSearchResult(entries: SpecEntry[], detail: ResponseDetail = 'compact'): McpToolResult {
  const allowed = entries.filter((e) => !e.excluded)

  if (allowed.length === 0) {
    return {
      content: [
        {
          type: 'text',
          text: [
            'No operations found matching your query. Try:',
            "- Browse by class: search({class:'read'}) or search({class:'write'})",
            "- Browse by tag: search({tag:'Analytics'})",
            "- Use a shorter, natural-language query, e.g. search({query:'spending by category'})",
            'Matching is per-word: at least ONE of your words must appear anywhere in an operation\'s id, summary, path, description, tags, or parameters; operations matching ALL your words rank first.',
          ].join('\n'),
        },
      ],
    }
  }

  const lines = allowed.map((e) => {
    // #1630: `manifestClass` alone told an agent whether an operation is 'read', 'write', or
    // 'destructive' — it did NOT tell them whether readExecute would actually ADMIT a 'read'
    // call. A class-'read' POST readExecute refuses (e.g. `runMCPRecipe`) used to render
    // identically to one it admits (e.g. `getTransactions`) — exactly what misled the #1630
    // reporter into believing "(read)" meant "usable via readExecute". Disclose the gap only
    // where it can exist (class 'read'); write/destructive operations are never admitted by
    // readExecute regardless, so a note there would be true of every single line and add noise
    // rather than information.
    let classLabel: string = e.manifestClass
    if (e.manifestClass === 'read') {
      const capability = getCapability(e.operationId)
      const admittedByReadExecute = !!capability && isReadExecuteAdmitted(capability, { method: e.method, path: e.path })
      classLabel = admittedByReadExecute ? 'read' : 'read, NOT admitted by readExecute — use writeExecute'
    }
    const parts = [
      `${e.method} ${e.path} (${classLabel})`,
      `  operationId: ${e.operationId}`,
      `  summary: ${e.summary}`,
    ]
    if (e.description) {
      parts.push(`  description: FULL API CONTRACT: ${e.description}`)
    }
    if (e.parameters?.length) {
      const paramLines = e.parameters.map(
        (p) =>
          `    ${p.name} (${p.in}${p.required ? ', required' : ''})${p.enum ? ` [${p.enum.join('|')}]` : ''}${p.description ? ` — ${p.description}` : ''}`
      )
      parts.push(`  params:\n${paramLines.join('\n')}`)
    }
    // Issue #1609 Task 6: render the request body shape so an MCP client can construct a
    // valid write body instead of guessing (the createRule incident — three guessed body
    // shapes, three bare 422s, no field detail available anywhere the client could reach).
    if (e.requestBody?.length) {
      // #1609 fix round 1: a field with no `type` in the index means the OpenAPI schema
      // genuinely declared none (e.g. RuleCondition.value: `{}` — a Go `any` field). Render
      // "any" (accepts any JSON value), not "unknown" — "unknown" reads identically whether
      // the extractor failed or the API truly accepts anything, which is exactly the
      // ambiguity this task exists to eliminate.
      const bodyLines = e.requestBody.map(
        (f) =>
          `    ${f.name} (${f.type ?? 'any'}${f.format ? `, format: ${f.format}` : ''}${f.required ? ', required' : ''})${f.enum ? ` [${f.enum.join('|')}]` : ''}${f.description ? ` — ${f.description}` : ''}`
      )
      parts.push(`  body:\n${bodyLines.join('\n')}`)
    }
    // Issue #1610a: render the response shape so an MCP client can discover what an operation
    // RETURNS without having to call it first. See the `f.type ?? 'any'` note above — the same
    // ambiguity applies here.
    const responseBody = responseFields(e, detail)
    if (e.responseShape) {
      const returnLines = responseBody?.map(
        (f) =>
          `    ${f.name} (${f.type ?? 'any'}${f.format ? `, format: ${f.format}` : ''}${f.required ? ', required' : ''})${f.enum ? ` [${f.enum.join('|')}]` : ''}${f.description ? ` — ${f.description}` : ''}`
      )
      const fields = returnLines?.length ? `\n${returnLines.join('\n')}` : ''
      parts.push(`  returns: ${renderResponseShape(e.responseShape)}${fields}`)
    }
    parts.push(`  tags: ${e.tags.join(', ')}`)
    return parts.join('\n')
  })

  return {
    content: [
      {
        type: 'text',
        text: `Response detail: ${detail}. Set top-level detail: "${detail === 'compact' ? 'full' : 'compact'}" on search to describe the other view; set detail on each readExecute/writeExecute call to request it. Discovery omits tooling $schema fields in both views. Compact may fall back to full with a warning if response shape changes.\nFound ${allowed.length} operation(s):\n\n${lines.join('\n\n')}`,
      },
    ],
  }
}

// ── Tool handler ─────────────────────────────────────────────────────────────

/**
 * Execute the search tool against a loaded spec index.
 * Returns the MCP tool result with formatted operation list.
 */
export function handleSearch(rawInput: unknown, specIndex: SpecIndex): McpToolResult {
  const input = parseSearchInput(rawInput)

  let entries: SpecEntry[]

  if (input.query || input.class) {
    entries = searchSpec(specIndex, input.query, {
      class: input.class,
      limit: input.limit,
      includeExcluded: false,
    })
    // Further filter by tag if provided
    if (input.tag) {
      entries = entries.filter((e) => e.tags.includes(input.tag!))
    }
  } else {
    entries = listSpecEntries(specIndex, {
      tag: input.tag,
      class: input.class,
    }).slice(0, input.limit)
  }

  return buildSearchResult(entries, input.detail)
}
