import { describe, expect, it } from 'bun:test'
import type { CapabilityEntry } from '../../manifest/capability-manifest'
import { isReadExecuteAdmitted, READ_ONLY_METHODS } from './read-execute-admission'

function capability(overrides: Partial<CapabilityEntry> = {}): CapabilityEntry {
  return {
    operationId: 'someOp',
    class: 'read',
    destructive: false,
    confirmationRequired: false,
    idempotent: true,
    excluded: false,
    ...overrides,
  }
}

const PREVIEW_CATEGORY_MATCHES_PATH = '/api/v1/budgets/categories/preview'

describe('isReadExecuteAdmitted', () => {
  it('admits a read-class, non-confirmation-gated GET', () => {
    expect(isReadExecuteAdmitted(capability(), { method: 'GET', path: '/x' })).toBe(true)
  })

  it('admits HEAD and OPTIONS the same as GET', () => {
    expect(isReadExecuteAdmitted(capability(), { method: 'HEAD', path: '/x' })).toBe(true)
    expect(isReadExecuteAdmitted(capability(), { method: 'OPTIONS', path: '/x' })).toBe(true)
  })

  it('refuses a write-class operation regardless of method', () => {
    expect(isReadExecuteAdmitted(capability({ class: 'write' }), { method: 'GET', path: '/x' })).toBe(false)
  })

  it('refuses a confirmation-gated operation regardless of class/method', () => {
    expect(isReadExecuteAdmitted(capability({ confirmationRequired: true }), { method: 'GET', path: '/x' })).toBe(
      false,
    )
  })

  it('refuses a read-class, non-confirmation-gated POST that is NOT an acknowledged safe-POST (runMCPRecipe shape)', () => {
    expect(
      isReadExecuteAdmitted(capability({ operationId: 'runMCPRecipe' }), {
        method: 'POST',
        path: '/api/v1/mcp/recipes/{id}/run',
      }),
    ).toBe(false)
  })

  it('admits a read-class, non-confirmation-gated POST that IS an acknowledged safe-POST at its acknowledged path', () => {
    expect(
      isReadExecuteAdmitted(capability({ operationId: 'previewCategoryMatches' }), {
        method: 'POST',
        path: PREVIEW_CATEGORY_MATCHES_PATH,
      }),
    ).toBe(true)
  })

  // Issue #1630 Task B4 RED: this is the exact scenario the task exists to close. Before this
  // change, admission was keyed on operationId alone — deleting the backend's x-mcp-safe-post
  // annotation, or moving the operation to a different verb, left it admitted forever. Anchoring
  // to the (operationId, method, path) tuple means a verb move alone drops it out of admission.
  it('REFUSES an acknowledged safe-POST operationId whose CURRENT spec entry moved to a different HTTP method', () => {
    expect(
      isReadExecuteAdmitted(capability({ operationId: 'previewCategoryMatches' }), {
        method: 'PUT',
        path: PREVIEW_CATEGORY_MATCHES_PATH,
      }),
    ).toBe(false)
  })

  it('REFUSES an acknowledged safe-POST operationId whose CURRENT spec entry moved to a different path', () => {
    expect(
      isReadExecuteAdmitted(capability({ operationId: 'previewCategoryMatches' }), {
        method: 'POST',
        path: '/api/v1/budgets/categories/preview/v2',
      }),
    ).toBe(false)
  })

  it('READ_ONLY_METHODS is exactly GET, HEAD, OPTIONS', () => {
    expect([...READ_ONLY_METHODS].sort()).toEqual(['GET', 'HEAD', 'OPTIONS'])
  })
})
