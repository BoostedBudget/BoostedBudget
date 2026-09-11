import { describe, expect, it } from 'bun:test'
import {
  ACKNOWLEDGED_SAFE_POST_OPERATIONS,
  ACKNOWLEDGED_SAFE_POST_OPERATION_IDS,
  extractSafePostOperationIds,
  extractSafePostOperations,
  findSafePostDrift,
  readSafePostOperationIds,
  readSafePostOperations,
} from './safe-post-extension'

describe('extractSafePostOperations', () => {
  it('returns the full (operationId, method, path) tuple for an operation carrying x-mcp-safe-post: true', () => {
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
    expect(extractSafePostOperations(spec)).toEqual([
      { operationId: 'previewCategoryMatches', method: 'POST', path: '/api/v1/budgets/categories/preview' },
    ])
  })

  it('ignores an operation where the extension is present but false', () => {
    const spec = {
      paths: {
        '/x': { post: { operationId: 'postX', 'x-mcp-safe-post': false } },
      },
    }
    expect(extractSafePostOperations(spec)).toEqual([])
  })

  it('ignores operations with no operationId even if the extension is set', () => {
    const spec = {
      paths: {
        '/x': { post: { 'x-mcp-safe-post': true } },
      },
    }
    expect(extractSafePostOperations(spec)).toEqual([])
  })

  it('returns a sorted list across multiple paths/methods', () => {
    const spec = {
      paths: {
        '/b': { post: { operationId: 'bOp', 'x-mcp-safe-post': true } },
        '/a': { post: { operationId: 'aOp', 'x-mcp-safe-post': true } },
      },
    }
    expect(extractSafePostOperations(spec)).toEqual([
      { operationId: 'aOp', method: 'POST', path: '/a' },
      { operationId: 'bOp', method: 'POST', path: '/b' },
    ])
  })

  it('tolerates a spec with no paths', () => {
    expect(extractSafePostOperations({})).toEqual([])
  })

  // Issue #1630 Task B4 RED: moving the SAME operationId to a different HTTP method must produce
  // a DIFFERENT tuple, so an admission/drift check comparing tuples (not just ids) treats it as
  // an unreviewed shape rather than a match.
  it('a tag on the same operationId but a different method produces a distinct tuple', () => {
    const spec = {
      paths: {
        '/api/v1/budgets/categories/preview': {
          put: { operationId: 'previewCategoryMatches', 'x-mcp-safe-post': true },
        },
      },
    }
    const found = extractSafePostOperations(spec)
    expect(found).toEqual([
      { operationId: 'previewCategoryMatches', method: 'PUT', path: '/api/v1/budgets/categories/preview' },
    ])
    expect(found).not.toEqual(ACKNOWLEDGED_SAFE_POST_OPERATIONS)
  })
})

describe('extractSafePostOperationIds (id-only projection)', () => {
  it('matches extractSafePostOperations mapped to operationId', () => {
    const spec = {
      paths: {
        '/b': { post: { operationId: 'bOp', 'x-mcp-safe-post': true } },
        '/a': { post: { operationId: 'aOp', 'x-mcp-safe-post': true } },
      },
    }
    expect(extractSafePostOperationIds(spec)).toEqual(['aOp', 'bOp'])
  })
})

describe('findSafePostDrift', () => {
  const op = (operationId: string, method = 'POST', path = `/${operationId}`) => ({ operationId, method, path })

  it('is empty in both directions when the spec-derived tuples match the acknowledged tuples exactly', () => {
    expect(findSafePostDrift([op('a'), op('b')], [op('a'), op('b')])).toEqual({ unacknowledged: [], stale: [] })
  })

  it('reports a spec-tagged tuple missing from the acknowledged list as unacknowledged', () => {
    expect(findSafePostDrift([op('a'), op('b'), op('c')], [op('a'), op('b')])).toEqual({
      unacknowledged: [op('c')],
      stale: [],
    })
  })

  // Issue #1630 Task B4 RED (the bidirectional half this task adds): an ACKNOWLEDGED tuple the
  // spec no longer tags must be reported as `stale`, not silently ignored the way the old
  // one-directional findUnacknowledgedSafePostOperations treated it. This is what makes deleting
  // the backend's x-mcp-safe-post annotation (or moving the op to a different verb) fail CI.
  it('reports an acknowledged tuple the spec no longer tags as stale', () => {
    expect(findSafePostDrift([op('a')], [op('a'), op('b')])).toEqual({
      unacknowledged: [],
      stale: [op('b')],
    })
  })

  it('reports an acknowledged operationId whose method changed as BOTH unacknowledged (new shape) and stale (old shape gone)', () => {
    const acknowledged = [op('previewCategoryMatches', 'POST', '/api/v1/budgets/categories/preview')]
    const specTagged = [op('previewCategoryMatches', 'PUT', '/api/v1/budgets/categories/preview')]
    expect(findSafePostDrift(specTagged, acknowledged)).toEqual({
      unacknowledged: specTagged,
      stale: acknowledged,
    })
  })

  it('handles both directions simultaneously', () => {
    const result = findSafePostDrift([op('a'), op('c')], [op('a'), op('b')])
    expect(result.unacknowledged).toEqual([op('c')])
    expect(result.stale).toEqual([op('b')])
  })
})

describe('real api/openapi.yaml', () => {
  it('readSafePostOperations finds exactly the acknowledged tuples, with no drift in either direction', () => {
    const found = readSafePostOperations()
    expect(found).toEqual(ACKNOWLEDGED_SAFE_POST_OPERATIONS)
    expect(findSafePostDrift(found, ACKNOWLEDGED_SAFE_POST_OPERATIONS)).toEqual({ unacknowledged: [], stale: [] })
  })

  it('readSafePostOperationIds matches ACKNOWLEDGED_SAFE_POST_OPERATION_IDS', () => {
    expect(readSafePostOperationIds()).toEqual(ACKNOWLEDGED_SAFE_POST_OPERATION_IDS)
  })
})
