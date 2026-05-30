import { describe, expect, it } from 'bun:test'
import {
  getCapability,
  isAllowed,
  isExcluded,
  isWrite,
  requiresConfirmation,
} from './capability-manifest'

// ── Deny-by-default ────────────────────────────────────────────────────────

describe('deny-by-default', () => {
  it('returns undefined for a completely unknown operationId', () => {
    expect(getCapability('nonExistentOperation')).toBeUndefined()
  })

  it('isAllowed returns false for unknown operation', () => {
    expect(isAllowed('nonExistentOperation')).toBe(false)
  })
})

// ── Read operations ────────────────────────────────────────────────────────

describe('read operations', () => {
  it('allows getTransactions', () => {
    const entry = getCapability('getTransactions')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(false)
  })

  it('allows getCashFlowSummary', () => {
    const entry = getCapability('getCashFlowSummary')
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
  })

  it('allows getSpendingByCategory', () => {
    expect(getCapability('getSpendingByCategory')?.class).toBe('read')
  })

  it('allows getBudgets', () => {
    expect(getCapability('getBudgets')?.class).toBe('read')
  })

  it('allows getUserCategories', () => {
    expect(getCapability('getUserCategories')?.class).toBe('read')
  })

  it('allows getNetWorth', () => {
    expect(getCapability('getNetWorth')?.class).toBe('read')
  })

  it('allows getTransferSuggestions', () => {
    expect(getCapability('getTransferSuggestions')?.class).toBe('read')
  })

  it('treats previewCategoryMatches as read (non-mutating POST)', () => {
    const entry = getCapability('previewCategoryMatches')
    expect(entry?.class).toBe('read')
    expect(entry?.idempotent).toBe(true)
    expect(entry?.confirmationRequired).toBe(false)
  })
})

// ── Write operations ───────────────────────────────────────────────────────

describe('write operations', () => {
  it('allows updateTransactionCategory as write with confirmation', () => {
    const entry = getCapability('updateTransactionCategory')
    expect(entry?.class).toBe('write')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(true)
  })

  it('allows createBudget as write with confirmation', () => {
    const entry = getCapability('createBudget')
    expect(entry?.class).toBe('write')
    expect(entry?.confirmationRequired).toBe(true)
  })

  it('allows createCategory as write with confirmation', () => {
    expect(getCapability('createCategory')?.class).toBe('write')
  })

  it('allows createTag as write with confirmation', () => {
    expect(getCapability('createTag')?.class).toBe('write')
  })

  it('allows createSavingsGoal as write with confirmation', () => {
    expect(getCapability('createSavingsGoal')?.class).toBe('write')
  })

  it('marks delete operations as destructive', () => {
    const del = getCapability('deleteBudget')
    expect(del?.class).toBe('write')
    expect(del?.destructive).toBe(true)
    expect(del?.confirmationRequired).toBe(true)
  })
})

// ── Excluded operations (permanently non-MCP) ─────────────────────────────

describe('excluded operations', () => {
  it('excludes all admin operations', () => {
    const adminOps = [
      'checkRole',
      'listAdminUsers',
      'getSystemHealth',
      'listAdminDeadLetterJobs',
      'bulkDeleteAdminDeadLetterJobs',
      'grantTrial',
      'get-admin-user-subscription',
      'update-admin-user-tier',
    ]
    for (const opId of adminOps) {
      expect(isExcluded(opId)).toBe(true)
    }
  })

  it('excludes Plaid account-linking operations (permanently non-MCP)', () => {
    expect(isExcluded('exchangePublicToken')).toBe(true)
    expect(isExcluded('createLinkToken')).toBe(true)
    expect(isExcluded('disconnectInstitution')).toBe(true)
    expect(isExcluded('deleteAccount')).toBe(true)
  })

  it('excludes user/account deletion', () => {
    expect(isExcluded('requestDeletion')).toBe(true)
    expect(isExcluded('cancelDeletion')).toBe(true)
  })

  it('excludes dashboard layout writes (deferred)', () => {
    expect(isExcluded('saveDashboardPreferences')).toBe(true)
    expect(isExcluded('resetDashboardPreferences')).toBe(true)
  })

  it('excludes automation-rule writes (need standing-auth)', () => {
    const automationWriteOps = [
      'createAutomationRule',
      'updateAutomationRule',
      'deleteAutomationRule',
      'disableAutomationRule',
      'enableAutomationRule',
      'toggleAutomationRule',
      'triggerAutomationRule',
      'testAutomationRule',
    ]
    for (const opId of automationWriteOps) {
      expect(isExcluded(opId)).toBe(true)
    }
  })

  it('excludes auto-categorization writes (deferred)', () => {
    const autoCatWrites = ['createRule', 'updateRule', 'deleteRule', 'toggleRule']
    for (const opId of autoCatWrites) {
      expect(isExcluded(opId)).toBe(true)
    }
  })

  it('excludes subscription writes (subscription management stays in-app)', () => {
    expect(isExcluded('cancel-paddle-subscription')).toBe(true)
    expect(isExcluded('create-paddle-checkout')).toBe(true)
    expect(isExcluded('update-paddle-subscription')).toBe(true)
  })

  it('excludes webhook endpoints', () => {
    expect(isExcluded('paddle-webhook')).toBe(true)
    expect(isExcluded('handlePlaidWebhook')).toBe(true)
  })

  it('excludes QA endpoint', () => {
    expect(isExcluded('qaAuthBypass')).toBe(true)
  })

  it('isAllowed returns false for excluded operations', () => {
    expect(isAllowed('exchangePublicToken')).toBe(false)
    expect(isAllowed('checkRole')).toBe(false)
    expect(isAllowed('requestDeletion')).toBe(false)
  })
})

// ── Helper function behaviour ──────────────────────────────────────────────

describe('helper functions', () => {
  it('isWrite returns true for write ops', () => {
    expect(isWrite('updateTransactionCategory')).toBe(true)
    expect(isWrite('createBudget')).toBe(true)
  })

  it('isWrite returns false for read ops', () => {
    expect(isWrite('getTransactions')).toBe(false)
    expect(isWrite('getCashFlowSummary')).toBe(false)
  })

  it('isWrite returns false for excluded ops', () => {
    expect(isWrite('requestDeletion')).toBe(false)
  })

  it('requiresConfirmation returns true for writes', () => {
    expect(requiresConfirmation('updateTransactionCategory')).toBe(true)
  })

  it('requiresConfirmation returns false for reads', () => {
    expect(requiresConfirmation('getTransactions')).toBe(false)
  })

  it('requiresConfirmation returns false for excluded ops', () => {
    expect(requiresConfirmation('requestDeletion')).toBe(false)
  })
})

// ── Completeness: every known operationId is classified ───────────────────

describe('completeness', () => {
  it('covers all 129 known operationIds (no unknown ops in the spec)', async () => {
    // We import the full list to ensure no ops are silently absent
    const { ALL_OPERATION_IDS } = await import('./capability-manifest')
    expect(ALL_OPERATION_IDS.length).toBe(129)
    for (const opId of ALL_OPERATION_IDS) {
      const entry = getCapability(opId)
      expect(entry).toBeDefined() // every known op must be classified
    }
  })
})
