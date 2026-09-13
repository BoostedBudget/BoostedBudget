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
import { parse } from 'yaml'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildSpecIndex, indexFromSpec } from './index-builder'
import type { SpecEntry } from './search-runtime'
import { responseFields } from './search-runtime'
import { getCapability } from '../manifest/capability-manifest'

let entries: SpecEntry[]

interface RawOracleOperation {
  operationId?: string
  responses?: Record<string, {
    content?: Record<string, { schema?: RawOracleSchema }>
  }>
}

interface RawOracleSchema {
  $ref?: string
  type?: string | string[]
  nullable?: boolean
  items?: RawOracleSchema
  properties?: Record<string, RawOracleSchema>
  additionalProperties?: unknown
}

type OracleResponseShape =
  | { type: 'array'; items: OracleResponseShape; nullable?: true }
  | { type: 'object' | 'string' | 'number' | 'integer' | 'boolean' | 'null'; nullable?: true }

function withOracleNullability(shape: OracleResponseShape, nullable: boolean): OracleResponseShape {
  return nullable ? { ...shape, nullable: true } : shape
}

/** Independent test oracle: deliberately does not import or call the production root analyzer. */
function expectedResponseShape(
  schema: RawOracleSchema,
  components: Record<string, RawOracleSchema>,
  seenRefs: Set<string> = new Set(),
): OracleResponseShape {
  const nullable = schema.nullable === true || (Array.isArray(schema.type) && schema.type.includes('null'))
  if (schema.$ref) {
    const name = schema.$ref.replace(/^#\/components\/schemas\//, '')
    if (seenRefs.has(name)) throw new Error(`oracle cycle at ${schema.$ref}`)
    const target = components[name]
    if (!target) throw new Error(`oracle unresolved ref ${schema.$ref}`)
    return withOracleNullability(
      expectedResponseShape(target, components, new Set([...seenRefs, name])),
      nullable,
    )
  }

  const declaredTypes = new Set(Array.isArray(schema.type) ? schema.type : schema.type ? [schema.type] : [])
  const nonNullTypes = [...declaredTypes].filter((candidate) => candidate !== 'null')
  if (nonNullTypes.length > 1) {
    throw new Error(`oracle unsupported response union ${nonNullTypes.join('|')}`)
  }
  const type = nonNullTypes[0]
    ?? (declaredTypes.has('null') ? 'null' : undefined)
    ?? (schema.properties || schema.additionalProperties ? 'object' : undefined)
  if (!type) throw new Error('oracle response root has no type')

  if (type === 'array') {
    if (!schema.items) throw new Error('oracle response array has no items')
    return withOracleNullability({
      type: 'array',
      items: expectedResponseShape(schema.items, components, seenRefs),
    }, nullable)
  }
  if (!['object', 'string', 'number', 'integer', 'boolean', 'null'].includes(type)) {
    throw new Error(`oracle unsupported response type ${type}`)
  }
  return withOracleNullability({
    type: type as Exclude<OracleResponseShape['type'], 'array'>,
  }, nullable && type !== 'null')
}

describe('#1634 projection-aware discovery', () => {
  it('expands only projected response paths and documents conditional derived fields', () => {
    const expanded = entries.map((e) => ({ ...e, compactResponseBody: responseFields(e, 'compact') }))
    const tx = expanded.find((e) => e.operationId === 'getTransactions')!
    expect(tx.responseBody?.some((f) => f.name === 'transactions[].account.plaid_account_id')).toBe(true)
    expect(tx.compactResponseBody?.some((f) => f.name === 'transactions[].account.plaid_account_id')).toBe(false)
    expect(tx.compactResponseBody?.some((f) => f.name === 'transactions[].account.name')).toBe(true)
    expect(tx.compactResponseBody?.find((f) => f.name === 'transactions[].effective_pfc_primary_display')).toMatchObject({ type: 'string|null' })
    expect(tx.compactResponseBody?.find((f) => f.name === 'transactions[].effective_pfc_primary_display')?.required).toBeUndefined()
    for (const name of ['limit', 'next_cursor', 'total_amount', 'total_count', 'transactions']) expect(tx.compactResponseBody?.some((f) => f.name === name)).toBe(true)
    const bare = expanded.find((e) => e.operationId === 'getTransactionByID')!
    expect(bare.compactResponseBody?.find((f) => f.name === 'reversed')).toMatchObject({ type: 'boolean', description: expect.stringContaining('provider_removed_posted') })
    expect(bare.compactResponseBody?.find((f) => f.name === 'reversed_at')).toMatchObject({ type: 'string', format: 'date-time' })
    expect(bare.compactResponseBody?.find((f) => f.name === 'reversed_at')?.required).toBeUndefined()
    const budget = expanded.find((e) => e.operationId === 'getBudgets')!
    expect(budget.compactResponseBody?.some((f) => f.name === 'categories[].allocated_amount')).toBe(true)
    const boost = expanded.find((e) => e.operationId === 'getBoosts')!
    expect(boost.compactResponseBody?.some((f) => f.name.startsWith('conditions[].'))).toBe(false)
    expect(boost.compactResponseBody?.some((f) => f.name.startsWith('actions[].'))).toBe(false)
    for (const [operationId, opaque] of [['getMCPRecipe', 'spec'], ['getUserTransactionAnnotationHistory', 'changes[].prior'], ['getUserBoostExecutions', 'executions[].output_data']]) {
      const entry = entries.find((e) => e.operationId === operationId)!
      for (const detail of ['compact', 'full'] as const) {
        const fields = responseFields(entry, detail)!
        expect(fields.some((field) => field.name === opaque)).toBe(true)
        expect(fields.some((field) => field.name.startsWith(`${opaque}.`))).toBe(false)
      }
    }
    expect(entries.find((e) => e.operationId === 'getCashFlowSummary')?.compactResponseBody).toBeUndefined()
    expect(entries.filter((e) => e.excluded).every((e) => !e.responseBody && !e.compactResponseBody)).toBe(true)
  })

  it('validates full schemas in the actual build path, including omitted tooling fields', async () => {
    const spec = parse(readFileSync(resolve(import.meta.dir, '../../../../api/openapi.yaml'), 'utf8'))
    spec.components.schemas.Account.properties.surprise = { type: 'string' }
    await expect(buildSpecIndex(spec)).rejects.toThrow('unclassified field surprise')
    delete spec.components.schemas.Account.properties.surprise
    delete spec.components.schemas.Transaction.properties.$schema
    await expect(buildSpecIndex(spec)).rejects.toThrow('stale field $schema')
    const changedContainer = parse(readFileSync(resolve(import.meta.dir, '../../../../api/openapi.yaml'), 'utf8'))
    changedContainer.components.schemas.GetTransactionsOutputBody.properties.transactions = { type: 'object' }
    await expect(buildSpecIndex(changedContainer)).rejects.toThrow('expected array schema')
  })

  it('regenerates the committed artifact exactly', async () => {
    expect(JSON.parse(readFileSync(resolve(import.meta.dir, 'generated/spec-index.json'), 'utf8'))).toEqual(await buildSpecIndex())
  })
})

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

// ── Issue #1630 Task 12a: format + anyOf extraction ───────────────────────────
//
// #1630's reporter could not discover that createBudget wanted a different date format from
// every read endpoint. Half the cause: `format` was never read off the body schema at all, so
// a plain `type: string, format: date` field rendered as bare `(string)` with no hint that it
// was a date. The other, WORSE half: Task 9 (domain.FlexibleDate, commit 85edc1fe) changed
// createBudget's start_date/end_date to publish `anyOf: [{type: string, format: date}, {type:
// string, format: date-time}]` — and the flattener, reading only a top-level `prop.type` /
// `prop.format`, drops BOTH for an anyOf field, rendering it with no type at all. These tests
// pin the fix on the REAL committed artifact (createBudget), not just a synthetic fixture —
// a synthetic-only test could pass while the real spec-index.json stayed broken.

describe('issue #1630 task 12a: format + anyOf extraction', () => {
  it('extracts format on a plain (non-anyOf) date field', () => {
    const specWithFormat = {
      paths: {
        '/api/v1/auto-categorization': {
          post: {
            operationId: 'createRule',
            summary: 'format test',
            requestBody: {
              content: { 'application/json': { schema: { $ref: '#/components/schemas/HasDate' } } },
            },
          },
        },
      },
      components: {
        schemas: {
          HasDate: {
            type: 'object',
            properties: {
              as_of: { type: 'string', format: 'date', description: 'A plain date field' },
            },
          },
        },
      },
    }
    const index = indexFromSpec(specWithFormat)
    const entry = index.entries.find((e) => e.operationId === 'createRule')
    const field = entry?.requestBody?.find((f) => f.name === 'as_of')
    expect(field?.type).toBe('string')
    expect(field?.format).toBe('date')
  })

  it('merges type and format across anyOf branches (the FlexibleDate shape)', () => {
    const specWithAnyOf = {
      paths: {
        '/api/v1/auto-categorization': {
          post: {
            operationId: 'createRule',
            summary: 'anyOf test',
            requestBody: {
              content: { 'application/json': { schema: { $ref: '#/components/schemas/HasFlexibleDate' } } },
            },
          },
        },
      },
      components: {
        schemas: {
          HasFlexibleDate: {
            type: 'object',
            properties: {
              start_date: {
                anyOf: [
                  { type: 'string', format: 'date' },
                  { type: 'string', format: 'date-time' },
                ],
                description: 'Budget start date',
              },
            },
          },
        },
      },
    }
    const index = indexFromSpec(specWithAnyOf)
    const entry = index.entries.find((e) => e.operationId === 'createRule')
    const field = entry?.requestBody?.find((f) => f.name === 'start_date')
    // Must NOT drop type entirely (the regression) — both anyOf branches agree on 'string'.
    expect(field?.type).toBe('string')
    expect(field?.format).toBe('date|date-time')
  })

  it('the committed createBudget.start_date carries type "string" and both formats (real artifact, not synthetic)', () => {
    const entry = entries.find((e) => e.operationId === 'createBudget')
    expect(entry).toBeDefined()

    const startDate = entry?.requestBody?.find((f) => f.name === 'start_date')
    expect(startDate?.type).toBe('string')
    expect(startDate?.format).toBe('date|date-time')
    expect(startDate?.required).toBe(true)

    const endDate = entry?.requestBody?.find((f) => f.name === 'end_date')
    expect(endDate?.type).toBe('string')
    expect(endDate?.format).toBe('date|date-time')
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

describe('issue #1610: explicit response-root shapes', () => {
  const operationSpec = (responses: Record<string, unknown>, operationId = 'getAccounts') => ({
    paths: {
      '/test': {
        get: { operationId, summary: 'Synthetic response shape', responses },
      },
    },
  })

  it('records a $ref-resolved JSON 200 object root', () => {
    const spec = {
      ...operationSpec({
        '200': {
          content: {
            'application/json': { schema: { $ref: '#/components/schemas/AccountRoot' } },
          },
        },
      }),
      components: {
        schemas: {
          AccountRoot: {
            type: 'object',
            properties: { id: { type: 'string' } },
          },
        },
      },
    }

    expect(indexFromSpec(spec).entries[0]?.responseShape).toEqual({ type: 'object' })
  })

  it('preserves nullable on a selected $ref sibling after resolving the root', () => {
    const spec = {
      ...operationSpec({
        '200': {
          content: {
            'application/json': {
              schema: { $ref: '#/components/schemas/AccountRoot', nullable: true },
            },
          },
        },
      }),
      components: {
        schemas: {
          AccountRoot: { type: 'object', properties: { id: { type: 'string' } } },
        },
      },
    }

    expect(indexFromSpec(spec).entries[0]?.responseShape).toEqual({
      type: 'object',
      nullable: true,
    })
  })

  it('uses JSON 201 when it is the only eligible success schema', () => {
    const index = indexFromSpec(operationSpec({
      '201': {
        content: { 'application/json': { schema: { type: 'string' } } },
      },
    }))

    expect(index.entries[0]?.responseShape).toEqual({ type: 'string' })
  })

  it('records a null-only response root exactly', () => {
    const index = indexFromSpec(operationSpec({
      '200': {
        content: { 'application/json': { schema: { type: ['null'] } } },
      },
    }))

    expect(index.entries[0]?.responseShape).toEqual({ type: 'null' })
    expect(expectedResponseShape({ type: ['null'] }, {})).toEqual({ type: 'null' })
  })

  it('rejects multiple distinct non-null root types and names the operationId', () => {
    const spec = operationSpec({
      '200': {
        content: {
          'application/json': { schema: { type: ['string', 'number', 'null'] } },
        },
      },
    })

    expect(() => indexFromSpec(spec)).toThrow(/getAccounts.*unsupported.*string.*number/i)
    expect(() => expectedResponseShape({ type: ['string', 'number', 'null'] }, {}))
      .toThrow(/unsupported response union string\|number/i)
  })

  it('does not spend the array nesting budget while following a valid ref chain', () => {
    const schemas = Object.fromEntries(Array.from({ length: 32 }, (_, index) => [
      `Root${index}`,
      index === 31
        ? { type: 'array', items: { type: 'string' } }
        : { $ref: `#/components/schemas/Root${index + 1}` },
    ]))
    const spec = {
      ...operationSpec({
        '200': {
          content: {
            'application/json': { schema: { $ref: '#/components/schemas/Root0' } },
          },
        },
      }),
      components: { schemas },
    }

    expect(indexFromSpec(spec).entries[0]?.responseShape).toEqual({
      type: 'array',
      items: { type: 'string' },
    })
  })

  it('rejects 33 response-root ref hops and names the operationId', () => {
    const schemas = Object.fromEntries(Array.from({ length: 33 }, (_, index) => [
      `Root${index}`,
      index === 32
        ? { type: 'object' }
        : { $ref: `#/components/schemas/Root${index + 1}` },
    ]))
    const spec = {
      ...operationSpec({
        '200': {
          content: {
            'application/json': { schema: { $ref: '#/components/schemas/Root0' } },
          },
        },
      }),
      components: { schemas },
    }

    expect(() => indexFromSpec(spec)).toThrow(/getAccounts.*exceeds 32 ref hops/i)
  })

  it('rejects 33 nested response-root arrays and names the operationId', () => {
    let root: RawOracleSchema = { type: 'string' }
    for (let depth = 0; depth < 33; depth += 1) {
      root = { type: 'array', items: root }
    }
    const spec = operationSpec({
      '200': {
        content: { 'application/json': { schema: root } },
      },
    })

    expect(() => indexFromSpec(spec)).toThrow(/getAccounts.*exceeds 32 nested arrays/i)
  })

  it('prefers a JSON 200 schema over a JSON 201 schema', () => {
    const index = indexFromSpec(operationSpec({
      '200': {
        content: { 'application/json': { schema: { type: 'object' } } },
      },
      '201': {
        content: { 'application/json': { schema: { type: 'string' } } },
      },
    }))

    expect(index.entries[0]?.responseShape).toEqual({ type: 'object' })
  })

  it('falls back to JSON 201 when 200 has no JSON schema', () => {
    const spec = {
      ...operationSpec({
        '200': {
          content: { 'text/plain': { schema: { type: 'string' } } },
        },
        '201': {
          content: {
            'application/json': { schema: { $ref: '#/components/schemas/CreatedAccount' } },
          },
        },
      }),
      components: {
        schemas: {
          CreatedAccount: {
            type: 'object',
            required: ['id'],
            properties: {
              id: { type: 'string' },
              display_name: { type: 'string' },
            },
          },
        },
      },
    }
    const entry = indexFromSpec(spec).entries[0]

    expect(entry?.responseShape).toEqual({ type: 'object' })
    expect(entry?.responseBody).toEqual([
      { name: 'id', type: 'string', required: true },
      { name: 'display_name', type: 'string' },
    ])
  })

  it('records a nullable array<string> root without inventing response fields', () => {
    const index = indexFromSpec(operationSpec({
      '200': {
        content: {
          'application/json': {
            schema: { type: ['array', 'null'], items: { type: 'string' } },
          },
        },
      },
    }))
    const entry = index.entries[0]

    expect(entry?.responseShape).toEqual({
      type: 'array',
      nullable: true,
      items: { type: 'string' },
    })
    expect(entry?.responseBody).toBeUndefined()
  })

  it('preserves nullability at every nested array node and flattens the innermost object', () => {
    const spec = {
      ...operationSpec({
        '200': {
          content: {
            'application/json': {
              schema: {
                type: ['array', 'null'],
                items: {
                  type: ['array', 'null'],
                  items: { $ref: '#/components/schemas/MerchantSummary' },
                },
              },
            },
          },
        },
      }, 'getBoosts'),
      components: {
        schemas: {
          MerchantSummary: {
            type: 'object',
            required: ['merchant_name'],
            properties: {
              merchant_name: { type: 'string' },
              total: { type: 'number' },
            },
          },
        },
      },
    }
    const entry = indexFromSpec(spec).entries[0]

    expect(entry?.responseShape).toEqual({
      type: 'array',
      nullable: true,
      items: {
        type: 'array',
        nullable: true,
        items: { type: 'object' },
      },
    })
    expect(entry?.responseBody?.map((field) => field.name)).toEqual(['merchant_name', 'total'])
  })

  it('omits shape and returns metadata for excluded or ineligible responses', () => {
    const spec = {
      paths: {
        '/excluded': { get: {
          operationId: 'checkRole', summary: 'Excluded',
          responses: { '200': { content: { 'application/json': { schema: { type: 'object' } } } } },
        } },
        '/non-json': { get: {
          operationId: 'getAccounts', summary: 'Non JSON',
          responses: { '200': { content: { 'text/plain': { schema: { type: 'string' } } } } },
        } },
        '/accepted': { get: {
          operationId: 'getBoosts', summary: 'Accepted only',
          responses: { '202': { content: { 'application/json': { schema: { type: 'object' } } } } },
        } },
        '/no-content': { get: {
          operationId: 'getCashFlowSummary', summary: 'No content', responses: { '204': {} },
        } },
        '/bodyless': { post: {
          operationId: 'createBudget', summary: 'Bodyless', responses: { '200': { description: 'OK' } },
        } },
      },
    }
    const entries = indexFromSpec(spec).entries

    for (const entry of entries) {
      expect(entry.responseShape, entry.operationId).toBeUndefined()
      expect(entry.responseBody, entry.operationId).toBeUndefined()
      expect(entry.compactResponseBody, entry.operationId).toBeUndefined()
    }
  })

  it('fails a selected unresolved response root and names the operationId', () => {
    const spec = operationSpec({
      '200': {
        content: {
          'application/json': { schema: { $ref: '#/components/schemas/MissingRoot' } },
        },
      },
    })

    expect(() => indexFromSpec(spec)).toThrow(/getAccounts.*unresolved/i)
  })

  it('fails a selected cyclic response root and names the operationId', () => {
    const spec = {
      ...operationSpec({
        '200': {
          content: {
            'application/json': { schema: { $ref: '#/components/schemas/CycleA' } },
          },
        },
      }),
      components: {
        schemas: {
          CycleA: { $ref: '#/components/schemas/CycleB' },
          CycleB: { $ref: '#/components/schemas/CycleA' },
        },
      },
    }

    expect(() => indexFromSpec(spec)).toThrow(/getAccounts.*cyclic/i)
  })

  it('pins representative real primitive and nested-array root shapes', () => {
    const available = entries.find((entry) => entry.operationId === 'getAvailableCategories')
    const plaid = entries.find((entry) => entry.operationId === 'getPlaidCategories')
    const duplicates = entries.find((entry) => entry.operationId === 'getMerchantDuplicateSuggestions')

    const nullableStringArray = {
      type: 'array',
      items: { type: 'string' },
      nullable: true,
    }
    expect(available?.responseShape).toEqual(nullableStringArray)
    expect(plaid?.responseShape).toEqual(nullableStringArray)
    expect(duplicates?.responseShape).toEqual({
      type: 'array',
      items: {
        type: 'array',
        items: { type: 'object' },
        nullable: true,
      },
      nullable: true,
    })
    expect(duplicates?.responseBody?.map((field) => field.name)).toEqual([
      'dominant_category',
      'effective_display_name',
      'has_override',
      'is_merged',
      'merchant_id',
      'merged_into_merchant_id',
      'normalized_name',
      'raw_variants',
      'total_spend',
      'transaction_count',
    ])
  })

  it('covers every allowed raw OpenAPI operation with an eligible JSON 200/201 schema', async () => {
    const rawSpec = parse(readFileSync(resolve(import.meta.dir, '../../../../api/openapi.yaml'), 'utf8'))
    const built = await buildSpecIndex()
    const components = rawSpec.components?.schemas as Record<string, RawOracleSchema>
    let eligibleCount = 0

    for (const pathItem of Object.values(rawSpec.paths ?? {}) as Record<string, unknown>[]) {
      for (const method of ['get', 'post', 'put', 'patch', 'delete']) {
        const operation = pathItem[method] as RawOracleOperation | undefined
        if (!operation?.operationId || getCapability(operation.operationId)?.excluded) continue
        const selected = operation.responses?.['200']?.content?.['application/json']?.schema
          ?? operation.responses?.['201']?.content?.['application/json']?.schema
        if (!selected) continue

        eligibleCount += 1
        const expected = expectedResponseShape(selected, components)
        const shape = built.entries.find((entry) => entry.operationId === operation.operationId)?.responseShape
        expect(shape, operation.operationId).toEqual(expected)
      }
    }

    expect(eligibleCount).toBeGreaterThan(0)
  })
})

// Artifact drift and size are separate guarantees: deterministic output can still bloat.
describe('issue #1610a: spec-index.json byte budget', () => {
  const __dirname = dirname(fileURLToPath(import.meta.url))
  const COMMITTED_INDEX_PATH = resolve(__dirname, 'generated/spec-index.json')

  it('bounds artifact size after selective expansion and compact metadata deduplication', () => {
    const raw = readFileSync(COMMITTED_INDEX_PATH, 'utf-8')
    const bytes = Buffer.byteLength(raw, 'utf-8')
    // #1610: the current response-root artifact is 550,623 bytes, leaving 9,377 bytes
    // (~1.7%) below this ceiling. Keep the assertion threshold-based so harmless JSON
    // ordering or description edits do not create a brittle exact-size pin.
    expect(bytes).toBeLessThan(560_000)
  })
})
