import { describe, expect, it } from 'bun:test'
import {
  ACKNOWLEDGED_LAZY_INIT_OPERATIONS,
  extractLazyInitOperationIds,
  findUnacknowledgedLazyInitOperations,
  readLazyInitOperationIds,
} from './lazy-init-extension'

// ── extractLazyInitOperationIds — the parsing layer ─────────────────────────
//
// A pure function over an already-parsed OpenAPI document so it can be exercised with
// synthetic fixtures (real api/openapi.yaml is covered separately below).

describe('extractLazyInitOperationIds', () => {
  it('returns operationIds whose operation carries x-mcp-lazy-init: true', () => {
    const spec = {
      paths: {
        '/api/v1/user/privacy': {
          get: { operationId: 'getPrivacySettings', 'x-mcp-lazy-init': true },
          put: { operationId: 'updatePrivacySettings' },
        },
        '/api/v1/transactions': {
          get: { operationId: 'getTransactions' },
        },
      },
    }
    expect(extractLazyInitOperationIds(spec)).toEqual(['getPrivacySettings'])
  })

  it('ignores an operation where the extension is present but false', () => {
    const spec = {
      paths: {
        '/x': { get: { operationId: 'getX', 'x-mcp-lazy-init': false } },
      },
    }
    expect(extractLazyInitOperationIds(spec)).toEqual([])
  })

  it('ignores operations with no operationId even if the extension is set', () => {
    const spec = {
      paths: {
        '/x': { get: { 'x-mcp-lazy-init': true } },
      },
    }
    expect(extractLazyInitOperationIds(spec)).toEqual([])
  })

  it('returns a sorted, de-duplicated-by-construction list across multiple paths/methods', () => {
    const spec = {
      paths: {
        '/b': { get: { operationId: 'bOp', 'x-mcp-lazy-init': true } },
        '/a': { post: { operationId: 'aOp', 'x-mcp-lazy-init': true } },
      },
    }
    expect(extractLazyInitOperationIds(spec)).toEqual(['aOp', 'bOp'])
  })

  it('tolerates a spec with no paths', () => {
    expect(extractLazyInitOperationIds({})).toEqual([])
  })

  // The mutant: a THIRD lazy-init-tagged operation that readExecute's admission property test
  // cannot see (it only checks non-destructive/non-confirmation-gated/read-verb, all of which a
  // lazy-init GET satisfies by design). Proving the extraction layer surfaces it is the
  // prerequisite for findUnacknowledgedLazyInitOperations (below) to actually catch it.
  it('surfaces a synthetic third lazy-init operation the acknowledged list does not know about', () => {
    const spec = {
      paths: {
        '/api/v1/user/privacy': {
          get: { operationId: 'getPrivacySettings', 'x-mcp-lazy-init': true },
        },
        '/api/v1/user/profile': {
          get: { operationId: 'getUserProfile', 'x-mcp-lazy-init': true },
        },
        '/api/v1/fake/thirdlazyop': {
          get: { operationId: 'getFakeThirdLazyInitOp', 'x-mcp-lazy-init': true },
        },
      },
    }
    const found = extractLazyInitOperationIds(spec)
    expect(found).toContain('getFakeThirdLazyInitOp')
    expect(findUnacknowledgedLazyInitOperations(found, ACKNOWLEDGED_LAZY_INIT_OPERATIONS)).toEqual([
      'getFakeThirdLazyInitOp',
    ])
  })
})

// ── findUnacknowledgedLazyInitOperations — the diff/scanner layer ───────────

describe('findUnacknowledgedLazyInitOperations', () => {
  it('is empty when the spec-derived list matches the acknowledged list exactly', () => {
    expect(findUnacknowledgedLazyInitOperations(['a', 'b'], ['a', 'b'])).toEqual([])
  })

  it('reports a spec-tagged operation missing from the acknowledged list', () => {
    expect(findUnacknowledgedLazyInitOperations(['a', 'b', 'c'], ['a', 'b'])).toEqual(['c'])
  })

  it('does NOT report an acknowledged operation the spec no longer tags (that is a separate — good — drift)', () => {
    expect(findUnacknowledgedLazyInitOperations(['a'], ['a', 'b'])).toEqual([])
  })
})

// ── Real api/openapi.yaml — the actual guard ────────────────────────────────

describe('readLazyInitOperationIds (real api/openapi.yaml)', () => {
  it('finds exactly the two acknowledged operations, and nothing unacknowledged', () => {
    const found = readLazyInitOperationIds()
    expect(found).toEqual(ACKNOWLEDGED_LAZY_INIT_OPERATIONS)
    expect(findUnacknowledgedLazyInitOperations(found, ACKNOWLEDGED_LAZY_INIT_OPERATIONS)).toEqual([])
  })
})
