import { describe, expect, it } from 'bun:test'
import {
  ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS,
  extractLegacyLazyInitOccurrences,
  extractReadSideEffectOperationIds,
  findUnacknowledgedReadSideEffectOperations,
  readLegacyLazyInitOccurrences,
  readReadSideEffectOperationIds,
} from './read-side-effect-extension'

describe('extractReadSideEffectOperationIds', () => {
  it('returns operationIds whose operation carries x-mcp-read-side-effect: true', () => {
    const spec = {
      paths: {
        '/api/v1/user/privacy': {
          get: { operationId: 'getPrivacySettings', 'x-mcp-read-side-effect': true },
          put: { operationId: 'updatePrivacySettings' },
        },
        '/api/v1/transactions': {
          get: { operationId: 'getTransactions' },
        },
      },
    }
    expect(extractReadSideEffectOperationIds(spec)).toEqual(['getPrivacySettings'])
  })

  it('ignores an operation where the extension is present but false', () => {
    const spec = {
      paths: {
        '/x': { get: { operationId: 'getX', 'x-mcp-read-side-effect': false } },
      },
    }
    expect(extractReadSideEffectOperationIds(spec)).toEqual([])
  })

  it('detects the removed x-mcp-lazy-init extension instead of accepting it as an alias', () => {
    const legacySpec = {
      paths: {
        '/legacy': { get: { operationId: 'legacyOperation', 'x-mcp-lazy-init': true } },
      },
    }
    expect(extractReadSideEffectOperationIds(legacySpec)).toEqual([])
    expect(extractLegacyLazyInitOccurrences(legacySpec)).toEqual([
      { operationId: 'legacyOperation', method: 'GET', path: '/legacy' },
    ])
  })

  it('ignores operations with no operationId even if the extension is set', () => {
    const spec = {
      paths: {
        '/x': { get: { 'x-mcp-read-side-effect': true } },
      },
    }
    expect(extractReadSideEffectOperationIds(spec)).toEqual([])
  })

  it('returns a sorted list across multiple paths and methods', () => {
    const spec = {
      paths: {
        '/b': { get: { operationId: 'bOp', 'x-mcp-read-side-effect': true } },
        '/a': { post: { operationId: 'aOp', 'x-mcp-read-side-effect': true } },
      },
    }
    expect(extractReadSideEffectOperationIds(spec)).toEqual(['aOp', 'bOp'])
  })

  it('tolerates a spec with no paths', () => {
    expect(extractReadSideEffectOperationIds({})).toEqual([])
  })

  it('surfaces a synthetic third read-side-effect operation missing from the acknowledged list', () => {
    const spec = {
      paths: {
        '/api/v1/user/privacy': {
          get: { operationId: 'getPrivacySettings', 'x-mcp-read-side-effect': true },
        },
        '/api/v1/user/profile': {
          get: { operationId: 'getUserProfile', 'x-mcp-read-side-effect': true },
        },
        '/api/v1/fake/third-side-effect': {
          get: { operationId: 'getFakeThirdSideEffect', 'x-mcp-read-side-effect': true },
        },
      },
    }
    const found = extractReadSideEffectOperationIds(spec)
    expect(found).toContain('getFakeThirdSideEffect')
    expect(findUnacknowledgedReadSideEffectOperations(
      found,
      ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS,
    )).toEqual(['getFakeThirdSideEffect'])
  })
})

describe('findUnacknowledgedReadSideEffectOperations', () => {
  it('is empty when the spec-derived list matches the acknowledged list exactly', () => {
    expect(findUnacknowledgedReadSideEffectOperations(['a', 'b'], ['a', 'b'])).toEqual([])
  })

  it('reports a spec-tagged operation missing from the acknowledged list', () => {
    expect(findUnacknowledgedReadSideEffectOperations(['a', 'b', 'c'], ['a', 'b'])).toEqual(['c'])
  })

  it('does not report an acknowledged operation the spec no longer tags', () => {
    expect(findUnacknowledgedReadSideEffectOperations(['a'], ['a', 'b'])).toEqual([])
  })
})

describe('readReadSideEffectOperationIds (real api/openapi.yaml)', () => {
  it('finds exactly the two acknowledged operations', () => {
    const found = readReadSideEffectOperationIds()
    expect(found).toEqual(ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS)
    expect(findUnacknowledgedReadSideEffectOperations(
      found,
      ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS,
    )).toEqual([])
    expect(readLegacyLazyInitOccurrences()).toEqual([])
  })
})
