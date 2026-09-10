/**
 * index-builder unit tests — R9-d (#1267)
 *
 * Calls `buildSpecIndex()` against the committed `api/openapi.yaml` to lock in
 * the R8-2b param-extraction rules:
 *   1. Description-only path params are kept (guards the `p.description ||` branch).
 *   2. `schema.enum` values are extracted into the `parameters[].enum` array.
 *   3. Array-type params with description (but no items.enum) are kept without an enum field.
 *   4. Every included parameter carries description OR enum (no silent passthrough).
 */

import { describe, expect, it, beforeAll } from 'bun:test'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildSpecIndex, indexFromSpec } from './index-builder'
import type { SpecEntry } from './search-runtime'

let entries: SpecEntry[]

beforeAll(async () => {
  const index = await buildSpecIndex()
  entries = index.entries
})

describe('buildSpecIndex param extraction (R8-2b regression)', () => {
  it('keeps description-only path params (description || branch)', () => {
    // getBoostExecutions.id: path param with description only, no enum.
    // Guards the `!!(p.description || ...)` filter keeping it.
    // (#1283: renamed from getRuleExecutions)
    const entry = entries.find((e) => e.operationId === 'getBoostExecutions')
    expect(entry).toBeDefined()

    const idParam = entry?.parameters?.find((p) => p.name === 'id' && p.in === 'path')
    expect(idParam).toBeDefined()
    expect(idParam?.description).toBeTruthy()
    expect(idParam?.enum).toBeUndefined()
  })

  it('extracts schema.enum values into the parameter enum array', () => {
    // getTransactions.sort_by has schema.enum: ['date','merchant','amount','category'].
    // Guards the `p.schema?.enum ?? ...` extraction branch.
    const entry = entries.find((e) => e.operationId === 'getTransactions')
    expect(entry).toBeDefined()

    const sortBy = entry?.parameters?.find((p) => p.name === 'sort_by')
    expect(sortBy).toBeDefined()
    expect(sortBy?.enum).toContain('date')
    expect(sortBy?.enum).toContain('amount')
    expect(sortBy?.enum).toContain('merchant')
  })

  it('keeps array-type params with description even when they have no items.enum', () => {
    // getBoosts.boost_type: type:array, has description, items have no enum.
    // The valid values are enforced handler-side (422); the OpenAPI schema uses plain items:{type:string}.
    // Guards that description-bearing array params are included but WITHOUT an enum field.
    // (#1283: operationId renamed from getAutomationRules; #1287: query param renamed rule_type→boost_type)
    const entry = entries.find((e) => e.operationId === 'getBoosts')
    expect(entry).toBeDefined()

    const boostTypeParam = entry?.parameters?.find((p) => p.name === 'boost_type')
    expect(boostTypeParam).toBeDefined()
    expect(boostTypeParam?.in).toBe('query')
    expect(boostTypeParam?.description).toBeTruthy()
    // No enum — handler-side validation means the spec doesn't enumerate values on items
    expect(boostTypeParam?.enum).toBeUndefined()
  })

  it('every included parameter carries at least a description or an enum (no silent passthrough)', () => {
    // The filter condition: `!!(p.description || p.schema?.enum || p.schema?.items?.enum)`
    // If a param slips through without either field, this catches it.
    for (const entry of entries) {
      for (const param of entry.parameters ?? []) {
        const hasDocumentation = !!(param.description ?? param.enum?.length)
        expect(
          hasDocumentation,
          `${entry.operationId}.${param.name} has neither description nor enum`
        ).toBe(true)
      }
    }
  })
})

// ── Issue #1503: unclassified operations must fail the build, not warn ───────

describe('issue #1503: fail-closed on unclassified operations', () => {
  // A SYNTHETIC spec, not the committed one. Every operation in api/openapi.yaml is classified by
  // definition once the manifest is complete, so a test that only calls buildSpecIndex() can never
  // reach the throw — it would stay green if the throw were reverted to the old warn-and-drop.
  // Codex's adversarial pass caught exactly that hole in the first version of this test.
  const specWithUnknownOp = {
    paths: {
      '/api/v1/transactions': { get: { operationId: 'getTransactions', summary: 'classified' } },
      '/api/v1/made-up': { post: { operationId: 'thisOperationIsNotInTheManifest', summary: 'unknown' } },
    },
  }

  it('THROWS on an operationId absent from the capability manifest', () => {
    expect(() => indexFromSpec(specWithUnknownOp)).toThrow(/thisOperationIsNotInTheManifest/)
  })

  it('names the offending method and path so the failure is actionable', () => {
    expect(() => indexFromSpec(specWithUnknownOp)).toThrow(/POST \/api\/v1\/made-up/)
  })

  it('points at the manifest as the fix', () => {
    expect(() => indexFromSpec(specWithUnknownOp)).toThrow(/capability-manifest\.ts/)
  })

  it('does NOT silently drop the unknown operation and return the rest', () => {
    // The pre-#1503 behaviour — warn + continue — would have returned a 1-entry index here
    // instead of throwing, which is precisely why the drift gate stayed green on a real omission.
    let returned: unknown
    try {
      returned = indexFromSpec(specWithUnknownOp)
    } catch {
      returned = 'threw'
    }
    expect(returned).toBe('threw')
  })

  it('still builds a fully classified spec without throwing', () => {
    const ok = indexFromSpec({
      paths: { '/api/v1/transactions': { get: { operationId: 'getTransactions', summary: 'ok' } } },
    })
    expect(ok.entries).toHaveLength(1)
    expect(ok.entries[0]?.operationId).toBe('getTransactions')
  })

  it('the committed spec is fully classified — buildSpecIndex resolves', async () => {
    // RED before #1503: listAdminPlaidOrphanedItems was absent from the manifest, so with the
    // throw in place this would have failed. Complements the synthetic tests above by pinning the
    // real artifact rather than the mechanism.
    await expect(buildSpecIndex()).resolves.toBeDefined()
  })

  it('every entry from the committed spec carries a manifest classification', () => {
    for (const entry of entries) {
      // #1569 Task 16 added the third class; keep this list exhaustive against OperationClass.
      expect(['read', 'write', 'destructive']).toContain(entry.manifestClass)
      expect(typeof entry.excluded).toBe('boolean')
    }
  })
})

// ── Issue #1609 Task 6: request body extraction ──────────────────────────────
//
// Real incident (2026-09-07): an MCP client guessed createRule's body shape three times
// (three different field layouts) and got a bare 422 with no field detail each time, then
// correctly stopped rather than keep firing speculative writes at a live financial account.
// search() exposed query params but never request bodies, so the schema was unlearnable.
// These tests pin the fix: requestBody schemas (including $ref-resolved, nested, array-of-
// object shapes) must flatten into the spec index in a token-frugal way.

describe('issue #1609 task 6: request body extraction', () => {
  // Mirrors the REAL CreateCategorizationRuleRequest / RuleCondition schemas in
  // api/openapi.yaml (createRule is already classified `write` in the manifest, so this
  // reuses that operationId rather than requiring a manifest change).
  const specWithRequestBody = {
    paths: {
      '/api/v1/auto-categorization': {
        post: {
          operationId: 'createRule',
          summary: 'Create a new categorization rule',
          requestBody: {
            content: {
              'application/json': {
                schema: { $ref: '#/components/schemas/CreateCategorizationRuleRequest' },
              },
            },
            required: true,
          },
        },
      },
      '/api/v1/transactions': {
        get: { operationId: 'getTransactions', summary: 'List transactions' },
      },
    },
    components: {
      schemas: {
        CreateCategorizationRuleRequest: {
          type: 'object',
          required: ['name', 'conditions'],
          properties: {
            name: { type: 'string', description: 'Human-readable rule name' },
            priority: { type: 'integer', description: 'Lower runs first' },
            tag_behavior: { type: 'string', enum: ['replace', 'append'] },
            conditions: {
              type: ['array', 'null'],
              items: { $ref: '#/components/schemas/RuleCondition' },
            },
          },
        },
        RuleCondition: {
          type: 'object',
          required: ['type', 'value'],
          properties: {
            type: { type: 'string', enum: ['merchant', 'amount', 'category'] },
            operator: { type: 'string', enum: ['contains', 'equals', 'gt', 'lt'] },
            value: {},
          },
        },
      },
    },
  }

  it('resolves a $ref requestBody into concrete fields, never a raw ref string', () => {
    const index = indexFromSpec(specWithRequestBody)
    const entry = index.entries.find((e) => e.operationId === 'createRule')
    expect(entry?.requestBody).toBeDefined()
    expect(entry?.requestBody?.some((f) => f.name === 'name')).toBe(true)
    expect(JSON.stringify(entry?.requestBody)).not.toContain('$ref')
  })

  it('marks required body fields correctly (name required, priority not)', () => {
    const index = indexFromSpec(specWithRequestBody)
    const entry = index.entries.find((e) => e.operationId === 'createRule')
    expect(entry?.requestBody?.find((f) => f.name === 'name')?.required).toBe(true)
    expect(entry?.requestBody?.find((f) => f.name === 'priority')?.required).toBeUndefined()
  })

  it('captures enum values on body fields', () => {
    const index = indexFromSpec(specWithRequestBody)
    const entry = index.entries.find((e) => e.operationId === 'createRule')
    expect(entry?.requestBody?.find((f) => f.name === 'tag_behavior')?.enum).toEqual(['replace', 'append'])
  })

  it('exposes array-of-object item fields with dotted names (the conditions[] case)', () => {
    const index = indexFromSpec(specWithRequestBody)
    const entry = index.entries.find((e) => e.operationId === 'createRule')

    const conditions = entry?.requestBody?.find((f) => f.name === 'conditions')
    expect(conditions?.type).toBe('array of object')
    expect(conditions?.required).toBe(true)

    const typeField = entry?.requestBody?.find((f) => f.name === 'conditions[].type')
    expect(typeField?.enum).toEqual(['merchant', 'amount', 'category'])
    expect(typeField?.required).toBe(true)

    const valueField = entry?.requestBody?.find((f) => f.name === 'conditions[].value')
    expect(valueField?.required).toBe(true)
  })

  it('an operation with no requestBody produces no requestBody key', () => {
    const index = indexFromSpec(specWithRequestBody)
    const entry = index.entries.find((e) => e.operationId === 'getTransactions')
    expect(entry?.requestBody).toBeUndefined()
  })

  it('does not infinitely recurse on a self-referential schema, and caps flattening depth', () => {
    const selfRefSpec = {
      paths: {
        '/api/v1/auto-categorization': {
          post: {
            operationId: 'createRule',
            summary: 'self-ref test',
            requestBody: {
              content: { 'application/json': { schema: { $ref: '#/components/schemas/SelfRef' } } },
            },
          },
        },
      },
      components: {
        schemas: {
          SelfRef: {
            type: 'object',
            properties: {
              child: { $ref: '#/components/schemas/SelfRef' },
              label: { type: 'string' },
            },
          },
        },
      },
    }
    expect(() => indexFromSpec(selfRefSpec)).not.toThrow()
  })

  it('the committed createRule operation carries a requestBody in the real generated index', () => {
    const entry = entries.find((e) => e.operationId === 'createRule')
    expect(entry?.requestBody?.some((f) => f.name === 'name' && f.required)).toBe(true)
    expect(entry?.requestBody?.some((f) => f.name === 'conditions')).toBe(true)
  })

  it('skips readOnly properties (Huma stamps a readOnly $schema field onto every body schema)', () => {
    const specWithReadOnly = {
      paths: {
        '/api/v1/auto-categorization': {
          post: {
            operationId: 'createRule',
            summary: 'readOnly test',
            requestBody: {
              content: { 'application/json': { schema: { $ref: '#/components/schemas/HasReadOnly' } } },
            },
          },
        },
      },
      components: {
        schemas: {
          HasReadOnly: {
            type: 'object',
            properties: {
              $schema: { type: 'string', readOnly: true, description: 'A URL to the JSON Schema.' },
              name: { type: 'string' },
            },
          },
        },
      },
    }
    const index = indexFromSpec(specWithReadOnly)
    const entry = index.entries.find((e) => e.operationId === 'createRule')
    expect(entry?.requestBody?.some((f) => f.name === '$schema')).toBe(false)
    expect(entry?.requestBody?.some((f) => f.name === 'name')).toBe(true)
  })
})

// ── Issue #1610a: response schema extraction ─────────────────────────────────
//
// search() exposed request-body schemas (#1609 Task 6) but nothing about what an operation
// RETURNS, so a client can construct a valid write body but has no way to discover a response
// shape without calling the operation first. Measured size cost drove the design: depth 2
// mirroring the request side would be +162% (486,582 bytes); depth 1 + allowed-only is the
// implemented row, +56% (289,705 bytes at measurement time — see the byte-budget test below for
// why that number is NOT pinned exactly).

describe('issue #1610a: response body extraction', () => {
  const specWithResponseBody = {
    paths: {
      '/api/v1/accounts': {
        get: {
          operationId: 'getAccounts',
          summary: 'Get an account',
          responses: {
            '200': {
              content: {
                'application/json': { schema: { $ref: '#/components/schemas/AccountResponse' } },
              },
            },
          },
        },
      },
      '/api/v1/boosts': {
        get: {
          operationId: 'getBoosts',
          summary: 'List boosts',
          responses: {
            '200': {
              content: {
                'application/json': {
                  schema: { type: 'array', items: { $ref: '#/components/schemas/BoostResponse' } },
                },
              },
            },
          },
        },
      },
      // excluded op — response schema must NOT be computed for it (allowed-only, −68 KB row)
      '/api/v1/admin/check-role': {
        get: { operationId: 'checkRole', summary: 'Check role', responses: {
          '200': { content: { 'application/json': { schema: { $ref: '#/components/schemas/AccountResponse' } } } },
        } },
      },
      '/api/v1/transactions': {
        get: { operationId: 'getTransactions', summary: 'No responses declared at all' },
      },
    },
    components: {
      schemas: {
        AccountResponse: {
          type: 'object',
          required: ['id', 'name'],
          properties: {
            $schema: { type: 'string', readOnly: true, description: 'A URL to the JSON Schema.' },
            id: { type: 'string', readOnly: true },
            name: { type: 'string' },
            balance: {
              type: 'object',
              properties: { current: { type: 'number' }, available: { type: 'number' } },
            },
          },
        },
        BoostResponse: {
          type: 'object',
          properties: {
            id: { type: 'string' },
            boost_type: { type: 'string', enum: ['transfer_suggestion', 'savings_goal'] },
          },
        },
      },
    },
  }

  it('extracts response fields, resolving $ref, never a raw ref string', () => {
    const index = indexFromSpec(specWithResponseBody)
    const entry = index.entries.find((e) => e.operationId === 'getAccounts')
    expect(entry?.responseBody).toBeDefined()
    expect(entry?.responseBody?.some((f) => f.name === 'name')).toBe(true)
    expect(JSON.stringify(entry?.responseBody)).not.toContain('$ref')
  })

  it('KEEPS a readOnly response field (opposite of the request-body policy)', () => {
    // `id` is readOnly:true — on the request side this would be dropped. On the response
    // side it is exactly the kind of field a client needs (a server-assigned identifier).
    const index = indexFromSpec(specWithResponseBody)
    const entry = index.entries.find((e) => e.operationId === 'getAccounts')
    expect(entry?.responseBody?.some((f) => f.name === 'id')).toBe(true)
  })

  it('skips ONLY the $schema tooling property by name, not by readOnly', () => {
    const index = indexFromSpec(specWithResponseBody)
    const entry = index.entries.find((e) => e.operationId === 'getAccounts')
    expect(entry?.responseBody?.some((f) => f.name === '$schema')).toBe(false)
  })

  it('caps nesting at depth 1 — a nested object is marked but not expanded', () => {
    const index = indexFromSpec(specWithResponseBody)
    const entry = index.entries.find((e) => e.operationId === 'getAccounts')
    const balance = entry?.responseBody?.find((f) => f.name === 'balance')
    expect(balance?.type).toBe('object')
    // Depth 2 would produce 'balance.current' — MUST be absent at MAX_RESPONSE_DEPTH = 1.
    expect(entry?.responseBody?.some((f) => f.name === 'balance.current')).toBe(false)
  })

  it('flattens a bare-array root response using the item schema, with no name prefix', () => {
    const index = indexFromSpec(specWithResponseBody)
    const entry = index.entries.find((e) => e.operationId === 'getBoosts')
    expect(entry?.responseBody?.some((f) => f.name === 'boost_type')).toBe(true)
    expect(entry?.responseBody?.find((f) => f.name === 'boost_type')?.enum).toEqual([
      'transfer_suggestion',
      'savings_goal',
    ])
  })

  it('computes NO response schema for an excluded operation (allowed-only)', () => {
    const index = indexFromSpec(specWithResponseBody)
    const entry = index.entries.find((e) => e.operationId === 'checkRole')
    expect(entry?.excluded).toBe(true)
    expect(entry?.responseBody).toBeUndefined()
  })

  it('an operation with no responses object produces no responseBody key', () => {
    const index = indexFromSpec(specWithResponseBody)
    const entry = index.entries.find((e) => e.operationId === 'getTransactions')
    expect(entry?.responseBody).toBeUndefined()
  })

  it('the committed spec produces responseBody for at least one real allowed GET operation', () => {
    const entry = entries.find((e) => e.operationId === 'getTransactions')
    expect(entry?.excluded).toBe(false)
    expect(entry?.responseBody?.length ?? 0).toBeGreaterThan(0)
  })
})

// ── Issue #1610a: the byte-budget regression test that did not exist ────────
//
// Nothing anywhere previously asserted a SIZE on spec-index.json — only drift gates (does the
// generated file match the committed one), which say nothing about whether that committed file
// is reasonably sized. This is what stops the next contributor from re-adding response depth 2
// (measured +162%, see the module doc comment) or dropping the allowed-only filter (+68 KB).
//
// ⚠ NOT pinned to the exact measured byte count (289,705 at measurement time): T1 in this same
// branch added a field to the Account response schema, which is this index's own input, so the
// true count has already moved and will keep moving as the API grows. This asserts a CEILING
// with headroom instead — its only job is to catch a gross regression (e.g. depth 2 creeping
// back in, which alone would roughly triple the file), not to pin today's exact size.
describe('issue #1610a: spec-index.json byte budget', () => {
  const __dirname = dirname(fileURLToPath(import.meta.url))
  const COMMITTED_INDEX_PATH = resolve(__dirname, 'generated/spec-index.json')

  it('stays under a generous ceiling — regression guard against depth-2 response schemas, not a precision pin', () => {
    const raw = readFileSync(COMMITTED_INDEX_PATH, 'utf-8')
    const bytes = Buffer.byteLength(raw, 'utf-8')
    // ~350 KB: headroom above the ~332 KB measured at depth 1 + allowed-only, comfortably below
    // the ~467-487 KB a depth-2 (with-or-without-descriptions) response schema would produce.
    // Raised from 330 KB in this round: effective_merchant condition, uncoercible_condition_value
    // quarantine reason, quarantine reason→behaviour mappings in doc tags, PFC closed-set endpoint
    // pointers, and minimum/maxLength constraints on savings-goal request schemas. This guard exists
    // to catch a depth-2 explosion, not to pin the index to the byte, so the headroom is restored
    // rather than the entry cut.
    expect(bytes).toBeLessThan(350_000)
  })
})
