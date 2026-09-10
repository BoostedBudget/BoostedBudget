import { describe, expect, it } from 'bun:test'
import { buildSearchResult, parseSearchInput } from './search'

describe('parseSearchInput', () => {
  it('accepts a plain query string', () => {
    const input = parseSearchInput({ query: 'spending by category' })
    expect(input.query).toBe('spending by category')
    expect(input.class).toBeUndefined()
    expect(input.tag).toBeUndefined()
    expect(input.limit).toBe(20)
  })

  it('accepts class filter', () => {
    const input = parseSearchInput({ query: 'budget', class: 'write' })
    expect(input.class).toBe('write')
  })

  // #1569 Task 16: the third capability class must be discoverable through the search tool —
  // an agent has to be able to ask "which of these destroy data?" before calling one, and to
  // filter them out. Without this the enum silently rejects it and the tier is invisible here.
  it('accepts the destructive class filter (#1569 Task 16)', () => {
    const input = parseSearchInput({ query: 'delete', class: 'destructive' })
    expect(input.class).toBe('destructive')
  })

  it('still rejects a class outside the manifest vocabulary', () => {
    expect(() => parseSearchInput({ query: 'delete', class: 'catastrophic' })).toThrow()
  })

  it('accepts tag filter', () => {
    const input = parseSearchInput({ query: '', tag: 'Analytics' })
    expect(input.tag).toBe('Analytics')
  })

  it('accepts custom limit', () => {
    const input = parseSearchInput({ query: 'trans', limit: 5 })
    expect(input.limit).toBe(5)
  })

  it('clamps limit to max 50', () => {
    const input = parseSearchInput({ query: 'x', limit: 999 })
    expect(input.limit).toBe(50)
  })

  it('throws on invalid class value', () => {
    expect(() => parseSearchInput({ query: 'x', class: 'admin' as unknown as 'read' })).toThrow()
  })
})

describe('buildSearchResult', () => {
  it('returns a text content block', () => {
    const result = buildSearchResult([
      {
        operationId: 'getTransactions',
        method: 'GET',
        path: '/api/v1/transactions',
        summary: 'List transactions',
        tags: ['Transactions'],
        manifestClass: 'read',
        excluded: false,
      },
    ])
    expect(result.content).toHaveLength(1)
    expect(result.content[0]?.type).toBe('text')
    expect(result.content[0]?.text).toContain('getTransactions')
    expect(result.content[0]?.text).toContain('GET')
  })

  it('returns empty result message when no matches', () => {
    const result = buildSearchResult([])
    expect(result.content[0]?.text).toContain('No operations found')
  })

  it('includes operation details', () => {
    const result = buildSearchResult([
      {
        operationId: 'createBudget',
        method: 'POST',
        path: '/api/v1/budgets',
        summary: 'Create a budget',
        tags: ['Budgets'],
        manifestClass: 'write',
        excluded: false,
      },
    ])
    expect(result.content[0]?.text).toContain('createBudget')
    expect(result.content[0]?.text).toContain('write')
    expect(result.content[0]?.text).toContain('POST /api/v1/budgets')
  })

  it('does not include excluded operations in output', () => {
    const result = buildSearchResult([
      {
        operationId: 'checkRole',
        method: 'GET',
        path: '/api/v1/admin/check-role',
        summary: 'Check role',
        tags: ['Admin'],
        manifestClass: 'read',
        excluded: true,
      },
    ])
    // Even if buildSearchResult receives an excluded entry (shouldn't happen), it filters them
    expect(result.content[0]?.text).not.toContain('checkRole')
  })

  // R8-2c: description and parameters improve AI discoverability in search output
  it('includes description when present (R8-2c)', () => {
    const result = buildSearchResult([
      {
        operationId: 'getTransferSuggestions',
        method: 'GET',
        path: '/api/v1/transfer-suggestions',
        summary: 'List transfer suggestions (filter by status)',
        description: 'Use status param: pending, accepted, completed.',
        tags: ['TransferSuggestions'],
        manifestClass: 'read',
        excluded: false,
      },
    ])
    expect(result.content[0]?.text).toContain('description:')
    expect(result.content[0]?.text).toContain('pending, accepted, completed')
  })

  it('includes parameter names and enum values when present (R8-2c)', () => {
    const result = buildSearchResult([
      {
        operationId: 'getTransferSuggestions',
        method: 'GET',
        path: '/api/v1/transfer-suggestions',
        summary: 'List transfer suggestions (filter by status)',
        description: 'Filter by status.',
        tags: ['TransferSuggestions'],
        manifestClass: 'read',
        excluded: false,
        parameters: [
          {
            name: 'status',
            in: 'query',
            description: 'Comma-separated statuses',
            enum: ['pending', 'accepted', 'completed', 'rejected', 'expired'],
            type: 'string',
          },
          {
            name: 'limit',
            in: 'query',
            description: 'Max results',
            type: 'integer',
          },
        ],
      },
    ])
    const text = result.content[0]?.text ?? ''
    expect(text).toContain('params:')
    expect(text).toContain('status')
    expect(text).toContain('pending|accepted|completed|rejected|expired')
    expect(text).toContain('limit')
  })

  it('omits description/params lines when not present (R8-2c)', () => {
    const result = buildSearchResult([
      {
        operationId: 'getTransactions',
        method: 'GET',
        path: '/api/v1/transactions',
        summary: 'List transactions',
        tags: ['Transactions'],
        manifestClass: 'read',
        excluded: false,
      },
    ])
    expect(result.content[0]?.text).not.toContain('description:')
    expect(result.content[0]?.text).not.toContain('params:')
  })

  // Issue #1609 Task 6: search() must render request body shape so an MCP client can
  // construct a valid write body instead of guessing (the createRule incident).
  it('renders a body: block for an operation with a requestBody (#1609)', () => {
    const result = buildSearchResult([
      {
        operationId: 'createRule',
        method: 'POST',
        path: '/api/v1/auto-categorization',
        summary: 'Create a new categorization rule',
        tags: ['Auto-Categorization'],
        manifestClass: 'write',
        excluded: false,
        requestBody: [
          { name: 'name', type: 'string', required: true, description: 'Human-readable rule name' },
          { name: 'priority', type: 'integer', description: 'Lower runs first' },
          { name: 'conditions', type: 'array of object', required: true },
          { name: 'conditions[].type', type: 'string', required: true, enum: ['merchant', 'amount', 'category'] },
          { name: 'conditions[].operator', type: 'string', required: true, enum: ['contains', 'equals', 'gt', 'lt'] },
          { name: 'conditions[].value', required: true },
        ],
      },
    ])
    const text = result.content[0]?.text ?? ''
    expect(text).toContain('body:')
    expect(text).toContain('name (string, required)')
    expect(text).toContain('conditions (array of object, required)')
    expect(text).toContain('conditions[].type')
    expect(text).toContain('merchant|amount|category')
  })

  // #1609 fix round 1: a field with no `type` (e.g. RuleCondition.value, declared as a
  // literal `{}` schema in api/openapi.yaml because the Go field is genuinely untyped `any`)
  // must render distinguishably from a tooling failure. The old `?? 'unknown'` fallback read
  // identically whether the extractor failed or the API truly accepts any JSON value — exactly
  // the ambiguity this task exists to eliminate, since `value` is a REQUIRED field on the very
  // operation (createRule) the incident was about.
  it('renders a typeless field (schema {}) as "any", never the literal "unknown" (#1609 fix round 1)', () => {
    const result = buildSearchResult([
      {
        operationId: 'createRule',
        method: 'POST',
        path: '/api/v1/auto-categorization',
        summary: 'Create a new categorization rule',
        tags: ['Auto-Categorization'],
        manifestClass: 'write',
        excluded: false,
        requestBody: [{ name: 'conditions[].value', required: true }],
      },
    ])
    const text = result.content[0]?.text ?? ''
    expect(text).toContain('conditions[].value (any, required)')
    expect(text).not.toContain('unknown')
  })

  // Issue #1630 Task 12a: search() must render `format` so a date field announces itself, and
  // must render an anyOf-merged format (multiple branches) legibly rather than dropping it.
  it('renders format on a body field, including a multi-branch anyOf format (#1630)', () => {
    const result = buildSearchResult([
      {
        operationId: 'createBudget',
        method: 'POST',
        path: '/api/v1/budgets',
        summary: 'Create a budget',
        tags: ['Budgets'],
        manifestClass: 'write',
        excluded: false,
        requestBody: [
          { name: 'start_date', type: 'string', format: 'date|date-time', required: true },
        ],
      },
    ])
    const text = result.content[0]?.text ?? ''
    expect(text).toContain('start_date (string, format: date|date-time, required)')
  })

  it('omits the body: block entirely when there is no requestBody (byte-identical to today for GETs)', () => {
    const result = buildSearchResult([
      {
        operationId: 'getTransactions',
        method: 'GET',
        path: '/api/v1/transactions',
        summary: 'List transactions',
        tags: ['Transactions'],
        manifestClass: 'read',
        excluded: false,
      },
    ])
    expect(result.content[0]?.text).not.toContain('body:')
  })

  // Issue #1610a: search() must render the response shape so a client can discover what an
  // operation RETURNS without calling it first.
  it('renders a returns: block for an operation with a responseBody (#1610a)', () => {
    const result = buildSearchResult([
      {
        operationId: 'getAccount',
        method: 'GET',
        path: '/api/v1/accounts/{id}',
        summary: 'Get an account',
        tags: ['Accounts'],
        manifestClass: 'read',
        excluded: false,
        responseBody: [
          { name: 'id', type: 'string', required: true },
          { name: 'name', type: 'string' },
          { name: 'status', type: 'string', enum: ['active', 'removed'] },
        ],
      },
    ])
    const text = result.content[0]?.text ?? ''
    expect(text).toContain('returns:')
    expect(text).toContain('id (string, required)')
    expect(text).toContain('status (string) [active|removed]')
  })

  it('omits the returns: block entirely when there is no responseBody', () => {
    const result = buildSearchResult([
      {
        operationId: 'getTransactions',
        method: 'GET',
        path: '/api/v1/transactions',
        summary: 'List transactions',
        tags: ['Transactions'],
        manifestClass: 'read',
        excluded: false,
      },
    ])
    expect(result.content[0]?.text).not.toContain('returns:')
  })

  it('renders returns: AFTER body: and BEFORE tags: — parts.push order is render order', () => {
    const result = buildSearchResult([
      {
        operationId: 'createRule',
        method: 'POST',
        path: '/api/v1/auto-categorization',
        summary: 'Create a rule',
        tags: ['Auto-Categorization'],
        manifestClass: 'write',
        excluded: false,
        requestBody: [{ name: 'name', type: 'string', required: true }],
        responseBody: [{ name: 'id', type: 'string', required: true }],
      },
    ])
    const text = result.content[0]?.text ?? ''
    const bodyIdx = text.indexOf('  body:')
    const returnsIdx = text.indexOf('  returns:')
    const tagsIdx = text.indexOf('  tags:')
    expect(bodyIdx).toBeGreaterThanOrEqual(0)
    expect(returnsIdx).toBeGreaterThan(bodyIdx)
    expect(tagsIdx).toBeGreaterThan(returnsIdx)
  })

  // #1630: `(read)` alone told an agent nothing about whether readExecute would actually admit
  // the call — search() and readExecute read the SAME manifest field but search() surfaced only
  // one of the three admission conditions. These pin the disclosure against the REAL manifest.
  describe('readExecute admission disclosure (#1630)', () => {
    it('an ordinary admitted GET still renders the plain "(read)" label', () => {
      const result = buildSearchResult([
        {
          operationId: 'getTransactions',
          method: 'GET',
          path: '/api/v1/transactions',
          summary: 'List transactions',
          tags: ['Transactions'],
          manifestClass: 'read',
          excluded: false,
        },
      ])
      expect(result.content[0]?.text).toContain('(read)')
      expect(result.content[0]?.text).not.toContain('NOT admitted')
    })

    it('runMCPRecipe (class read, POST, refused by readExecute) discloses that it is NOT admitted', () => {
      const result = buildSearchResult([
        {
          operationId: 'runMCPRecipe',
          method: 'POST',
          path: '/api/v1/mcp/recipes/{id}/run',
          summary: 'Run an MCP recipe',
          tags: ['MCP Recipes'],
          manifestClass: 'read',
          excluded: false,
        },
      ])
      const text = result.content[0]?.text ?? ''
      expect(text).toContain('runMCPRecipe')
      expect(text).toContain('read, NOT admitted by readExecute')
      expect(text).toContain('writeExecute')
    })

    it('previewCategoryMatches (class read, POST, acknowledged safe-POST) discloses plain "(read)" — it IS admitted', () => {
      const result = buildSearchResult([
        {
          operationId: 'previewCategoryMatches',
          method: 'POST',
          path: '/api/v1/budgets/categories/preview',
          summary: 'Preview category matches',
          tags: ['Budget'],
          manifestClass: 'read',
          excluded: false,
        },
      ])
      const text = result.content[0]?.text ?? ''
      expect(text).toContain('previewCategoryMatches')
      expect(text).toContain('(read)')
      expect(text).not.toContain('NOT admitted')
    })

    it('a write-class operation is never decorated with an admission note (it is always refused, and saying so on every line is noise)', () => {
      const result = buildSearchResult([
        {
          operationId: 'createBudget',
          method: 'POST',
          path: '/api/v1/budgets',
          summary: 'Create a budget',
          tags: ['Budgets'],
          manifestClass: 'write',
          excluded: false,
        },
      ])
      const text = result.content[0]?.text ?? ''
      expect(text).toContain('(write)')
      expect(text).not.toContain('NOT admitted')
    })
  })
})
