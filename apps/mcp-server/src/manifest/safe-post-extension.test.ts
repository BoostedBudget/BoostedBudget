import { describe, expect, it } from 'bun:test'
import {
  ACKNOWLEDGED_SAFE_POST_OPERATIONS,
  extractSafePostOperationIds,
  findUnacknowledgedSafePostOperations,
  readSafePostOperationIds,
} from './safe-post-extension'

describe('extractSafePostOperationIds', () => {
  it('returns operationIds whose operation carries x-mcp-safe-post: true', () => {
    const spec = {
      paths: {
        '/api/v1/budgets/categories/preview': {
          post: { operationId: 'previewCategoryMatches', 'x-mcp-safe-post': true },
        },
        '/api/v1/mcp/recipes/{id}/run': {
          post: { operationId: 'runMCPRecipe' },
        },
        '/api/v1/transactions': {
          get: { operationId: 'getTransactions' },
        },
      },
    }
    expect(extractSafePostOperationIds(spec)).toEqual(['previewCategoryMatches'])
  })

  it('ignores an operation where the extension is present but false', () => {
    const spec = {
      paths: {
        '/x': { post: { operationId: 'postX', 'x-mcp-safe-post': false } },
      },
    }
    expect(extractSafePostOperationIds(spec)).toEqual([])
  })

  it('ignores operations with no operationId even if the extension is set', () => {
    const spec = {
      paths: {
        '/x': { post: { 'x-mcp-safe-post': true } },
      },
    }
    expect(extractSafePostOperationIds(spec)).toEqual([])
  })

  it('returns a sorted, de-duplicated-by-construction list across multiple paths/methods', () => {
    const spec = {
      paths: {
        '/b': { post: { operationId: 'bOp', 'x-mcp-safe-post': true } },
        '/a': { post: { operationId: 'aOp', 'x-mcp-safe-post': true } },
      },
    }
    expect(extractSafePostOperationIds(spec)).toEqual(['aOp', 'bOp'])
  })

  it('tolerates a spec with no paths', () => {
    expect(extractSafePostOperationIds({})).toEqual([])
  })

  it('surfaces a synthetic second safe-post operation the acknowledged list does not know about', () => {
    const spec = {
      paths: {
        '/api/v1/budgets/categories/preview': {
          post: { operationId: 'previewCategoryMatches', 'x-mcp-safe-post': true },
        },
        '/api/v1/fake/thirdsafepost': {
          post: { operationId: 'postFakeSecondSafePostOp', 'x-mcp-safe-post': true },
        },
      },
    }
    const found = extractSafePostOperationIds(spec)
    expect(found).toContain('postFakeSecondSafePostOp')
    expect(findUnacknowledgedSafePostOperations(found, ACKNOWLEDGED_SAFE_POST_OPERATIONS)).toEqual([
      'postFakeSecondSafePostOp',
    ])
  })
})

describe('findUnacknowledgedSafePostOperations', () => {
  it('is empty when the spec-derived list matches the acknowledged list exactly', () => {
    expect(findUnacknowledgedSafePostOperations(['a', 'b'], ['a', 'b'])).toEqual([])
  })

  it('reports a spec-tagged operation missing from the acknowledged list', () => {
    expect(findUnacknowledgedSafePostOperations(['a', 'b', 'c'], ['a', 'b'])).toEqual(['c'])
  })

  it('does NOT report an acknowledged operation the spec no longer tags (that is a separate — good — drift)', () => {
    expect(findUnacknowledgedSafePostOperations(['a'], ['a', 'b'])).toEqual([])
  })
})

describe('readSafePostOperationIds (real api/openapi.yaml)', () => {
  it('finds exactly the one acknowledged operation, and nothing unacknowledged', () => {
    const found = readSafePostOperationIds()
    expect(found).toEqual(ACKNOWLEDGED_SAFE_POST_OPERATIONS)
    expect(findUnacknowledgedSafePostOperations(found, ACKNOWLEDGED_SAFE_POST_OPERATIONS)).toEqual([])
  })
})
