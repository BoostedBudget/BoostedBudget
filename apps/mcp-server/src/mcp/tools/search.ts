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
import { searchSpec, listSpecEntries } from '../../spec/search-runtime'
import type { SpecEntry, SpecIndex } from '../../spec/search-runtime'

// ── Input schema ────────────────────────────────────────────────────────────

const SearchInputSchema = z.object({
  query: z.string().default(''),
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

/**
 * Format search results into an MCP text content block.
 * Excluded operations are filtered out before rendering.
 */
export function buildSearchResult(entries: SpecEntry[]): McpToolResult {
  const allowed = entries.filter((e) => !e.excluded)

  if (allowed.length === 0) {
    return {
      content: [{ type: 'text', text: 'No operations found matching your query.' }],
    }
  }

  const lines = allowed.map((e) => {
    const parts = [
      `${e.method} ${e.path} (${e.manifestClass})`,
      `  operationId: ${e.operationId}`,
      `  summary: ${e.summary}`,
    ]
    if (e.description) {
      parts.push(`  description: ${e.description}`)
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
    if (e.responseBody?.length) {
      const returnLines = e.responseBody.map(
        (f) =>
          `    ${f.name} (${f.type ?? 'any'}${f.format ? `, format: ${f.format}` : ''}${f.required ? ', required' : ''})${f.enum ? ` [${f.enum.join('|')}]` : ''}${f.description ? ` — ${f.description}` : ''}`
      )
      parts.push(`  returns:\n${returnLines.join('\n')}`)
    }
    parts.push(`  tags: ${e.tags.join(', ')}`)
    return parts.join('\n')
  })

  return {
    content: [
      {
        type: 'text',
        text: `Found ${allowed.length} operation(s):\n\n${lines.join('\n\n')}`,
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

  return buildSearchResult(entries)
}
