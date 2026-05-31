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
  class: z.enum(['read', 'write']).optional(),
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

  const lines = allowed.map((e) =>
    `${e.method} ${e.path} (${e.manifestClass})\n  operationId: ${e.operationId}\n  summary: ${e.summary}\n  tags: ${e.tags.join(', ')}`,
  )

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
