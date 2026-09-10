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

describe('isReadExecuteAdmitted', () => {
  it('admits a read-class, non-confirmation-gated GET', () => {
    expect(isReadExecuteAdmitted(capability(), 'GET')).toBe(true)
  })

  it('admits HEAD and OPTIONS the same as GET', () => {
    expect(isReadExecuteAdmitted(capability(), 'HEAD')).toBe(true)
    expect(isReadExecuteAdmitted(capability(), 'OPTIONS')).toBe(true)
  })

  it('refuses a write-class operation regardless of method', () => {
    expect(isReadExecuteAdmitted(capability({ class: 'write' }), 'GET')).toBe(false)
  })

  it('refuses a confirmation-gated operation regardless of class/method', () => {
    expect(isReadExecuteAdmitted(capability({ confirmationRequired: true }), 'GET')).toBe(false)
  })

  it('refuses a read-class, non-confirmation-gated POST that is NOT an acknowledged safe-POST (runMCPRecipe shape)', () => {
    expect(isReadExecuteAdmitted(capability({ operationId: 'runMCPRecipe' }), 'POST')).toBe(false)
  })

  it('admits a read-class, non-confirmation-gated POST that IS an acknowledged safe-POST', () => {
    expect(isReadExecuteAdmitted(capability({ operationId: 'previewCategoryMatches' }), 'POST')).toBe(true)
  })

  it('READ_ONLY_METHODS is exactly GET, HEAD, OPTIONS', () => {
    expect([...READ_ONLY_METHODS].sort()).toEqual(['GET', 'HEAD', 'OPTIONS'])
  })
})
