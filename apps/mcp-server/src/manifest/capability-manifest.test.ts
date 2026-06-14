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

// ── Recipe operations ──────────────────────────────────────────────────────

describe('recipe operations', () => {
  it('classifies runMCPRecipe as READ (v1 recipes are read-only compositions)', () => {
    // GAP-R3-10: runMCPRecipe was misclassified as write, causing host-proxy to demand
    // a confirmation token that the recipe.run tool has no input field for.
    // v1 recipes can only compose read-class ops, so running them is a read operation.
    const entry = getCapability('runMCPRecipe')
    expect(entry?.class).toBe('read')
    expect(entry?.confirmationRequired).toBe(false)
    expect(entry?.excluded).toBe(false)
  })

  it('classifies listMCPRecipes as read', () => {
    expect(getCapability('listMCPRecipes')?.class).toBe('read')
  })

  it('classifies getMCPRecipe as read', () => {
    expect(getCapability('getMCPRecipe')?.class).toBe('read')
  })

  it('classifies createMCPRecipe as write (confirmation required)', () => {
    const entry = getCapability('createMCPRecipe')
    expect(entry?.class).toBe('write')
    expect(entry?.confirmationRequired).toBe(true)
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

  it('excludes boost writes (need standing-auth)', () => {
    // #1283: renamed from automation-rule writes
    const boostWriteOps = [
      'createBoost',
      'updateBoost',
      'deleteBoost',
      'disableBoost',
      'enableBoost',
      'toggleBoost',
      'triggerBoost',
      'testBoost',
    ]
    for (const opId of boostWriteOps) {
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
  it('covers all 169 known operationIds (no unknown ops in the spec)', async () => {
    // We import the full list to ensure no ops are silently absent
    // +8 from Issue #138: getHoldings, getPortfolioSummary, getPortfolioHistory,
    //   getInvestmentTransactions, getPortfolioAllocationBreakdown, getTopMovers,
    //   getInvestmentGainLossSummary, getInvestmentIncomeSummary
    // +1 from Issue #1300: getSavingsProgressSummary (basis-aware analytics, Codex C11)
    // +4 from Issue #1300 M20: listSavingsGoalContributions, logSavingsGoalContribution,
    //   confirmSavingsGoalContribution, deleteSavingsGoalContribution
    const { ALL_OPERATION_IDS } = await import('./capability-manifest')
    expect(ALL_OPERATION_IDS.length).toBe(169)
    for (const opId of ALL_OPERATION_IDS) {
      const entry = getCapability(opId)
      expect(entry).toBeDefined() // every known op must be classified
    }
  })
})

// ── Issue #1210: new transaction review/recategorize operations ──────────────

describe('issue #1210: review workflow operations', () => {
  it('classifies getTransactionReviewSummary as read (AI-tool-callable)', () => {
    const entry = getCapability('getTransactionReviewSummary')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(false)
    expect(entry?.idempotent).toBe(true)
  })

  it('classifies bulkUpdateTransactionCategory as write (confirmation required, idempotent)', () => {
    const entry = getCapability('bulkUpdateTransactionCategory')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('write')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(true)
    expect(entry?.idempotent).toBe(true)
    expect(entry?.destructive).toBe(false)
  })
})

// ── Issue #972: savings transfer automation operations ─────────────────────

describe('issue #972: savings transfer automation operations', () => {
  it('classifies getSavingsImpactAggregate as read (public aggregate, AI-tool-callable)', () => {
    const entry = getCapability('getSavingsImpactAggregate')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(false)
    expect(entry?.idempotent).toBe(true)
  })

  it('classifies getSavingsImpactMe as read (authed per-user, monitoring)', () => {
    const entry = getCapability('getSavingsImpactMe')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(false)
    expect(entry?.idempotent).toBe(true)
  })

  it('classifies confirmTransferSuggestionMatch as write (idempotent, carries idempotency key)', () => {
    const entry = getCapability('confirmTransferSuggestionMatch')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('write')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(true)
    expect(entry?.idempotent).toBe(true)
    expect(entry?.destructive).toBe(false)
  })

  // GAP CJ: declineTransferSuggestionMatch ships in the manifest but was unasserted — regression guard.
  it('classifies declineTransferSuggestionMatch as write (idempotent, releases reservation)', () => {
    const entry = getCapability('declineTransferSuggestionMatch')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('write')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(true)
    expect(entry?.idempotent).toBe(true)
    expect(entry?.destructive).toBe(false)
  })

  // R3-4 (#1267): cancelTransferSuggestion was in Go manifest + TS manifest but missing from
  // the regenerated facade + spec-index. This assertion guards the regenerated artifacts.
  it('classifies cancelTransferSuggestion as write with confirmation', () => {
    const entry = getCapability('cancelTransferSuggestion')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('write')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(true)
    expect(entry?.idempotent).toBe(true)
    expect(entry?.destructive).toBe(false)
  })

  it('isAllowed returns true for all five new operations', () => {
    expect(isAllowed('getSavingsImpactAggregate')).toBe(true)
    expect(isAllowed('getSavingsImpactMe')).toBe(true)
    expect(isAllowed('confirmTransferSuggestionMatch')).toBe(true)
    expect(isAllowed('declineTransferSuggestionMatch')).toBe(true)
    expect(isAllowed('cancelTransferSuggestion')).toBe(true)
  })

  it('isWrite returns true for write ops and false for reads', () => {
    expect(isWrite('confirmTransferSuggestionMatch')).toBe(true)
    expect(isWrite('declineTransferSuggestionMatch')).toBe(true)
    expect(isWrite('cancelTransferSuggestion')).toBe(true)
    expect(isWrite('getSavingsImpactAggregate')).toBe(false)
    expect(isWrite('getSavingsImpactMe')).toBe(false)
  })
})

// ── Issue #138: investment portfolio operations ────────────────────────────

describe('issue #138: investment portfolio operations', () => {
  const coreOps = [
    'getHoldings',
    'getPortfolioSummary',
    'getPortfolioHistory',
    'getInvestmentTransactions',
  ]
  const analyticsOps = [
    'getPortfolioAllocationBreakdown',
    'getTopMovers',
    'getInvestmentGainLossSummary',
    'getInvestmentIncomeSummary',
  ]

  it('classifies all 8 investment ops as read and allowed', () => {
    for (const opId of [...coreOps, ...analyticsOps]) {
      const entry = getCapability(opId)
      expect(entry).toBeDefined()
      expect(entry?.class).toBe('read')
      expect(entry?.excluded).toBe(false)
      expect(isAllowed(opId)).toBe(true)
    }
  })

  it('getTransactions remains read (unified-feed response shape change is MCP-safe)', () => {
    const entry = getCapability('getTransactions')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
  })
})

// ── Issue #1300: savings progress analytics (Codex C11) ────────────────────

describe('issue #1300: savings progress summary analytics', () => {
  it('classifies getSavingsProgressSummary as read (basis-aware analytics, AI-tool-callable)', () => {
    const entry = getCapability('getSavingsProgressSummary')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(false)
    expect(entry?.idempotent).toBe(true)
  })
})
