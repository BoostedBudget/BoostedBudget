import { describe, expect, it } from 'bun:test'
import {
  ALL_OPERATION_IDS,
  getCapability,
  isAllowed,
  destroysData,
  isDestructive,
  isExcluded,
  isWrite,
  requiresConfirmation
} from './capability-manifest'

// ── Deny-by-default ────────────────────────────────────────────────────────

describe('deny-by-default', () => {
  for (const [operationId, operationClass] of [
    ['getAdminRevenueCatHealth', 'read'],
    ['listAdminRevenueCatWebhooks', 'read'],
    ['getAdminRevenueCatUser', 'read'],
    ['replayAdminRevenueCatWebhook', 'write'],
    ['requestAdminRevenueCatGrant', 'write'],
    ['retryAdminRevenueCatOperation', 'write'],
    ['refreshAdminRevenueCatUser', 'write'],
    ['getRevenueCatSubscription', 'read'],
    ['refreshRevenueCatSubscription', 'write'],
    ['beginRevenueCatCheckout', 'write'],
    ['releaseRevenueCatCheckout', 'write'],
    ['revenuecat-webhook', 'write'],
  ] as const) {
    it(`explicitly excludes ${operationId} without exposing subscription controls`, () => {
      const entry = getCapability(operationId)
      expect(entry).toBeDefined()
      expect(entry?.class).toBe(operationClass)
      expect(entry?.excluded).toBe(true)
      expect(entry?.excludedReason).toBeTruthy()
      expect(isAllowed(operationId)).toBe(false)
    })
  }

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

  // ⚠ Rewritten by Issue #1569 Task 16. This used to read "marks delete operations as
  // destructive" and assert deleteBudget.destructive === true. deleteBudget no longer destroys
  // anything: budget_repository.go Delete tombstones the budget and its categories. The
  // genuinely destructive verbs are pinned in the 'destructive class' suite at the bottom.
  it('marks the soft-deleting deleteBudget as a confirmation-gated, non-destructive write', () => {
    const del = getCapability('deleteBudget')
    expect(del?.class).toBe('write')
    expect(del?.destructive).toBe(false)
    expect(del?.confirmationRequired).toBe(true)
  })
})

// ── Recipe operations ──────────────────────────────────────────────────────

describe('recipe operations', () => {
  it('classifies runMCPRecipe as READ (v1 recipes are read-only compositions)', () => {
    // GAP-R3-10: runMCPRecipe was misclassified as write, causing host-proxy to demand
    // a confirmation token that the dedicated recipe tool had no input field for.
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
  it('keeps the frontend-only budget plan report outside MCP capabilities', () => {
    const entry = getCapability('getBudgetPlanVsActual')
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(true)
    expect(entry?.excludedReason).toContain('Frontend-only')
    expect(isAllowed('getBudgetPlanVsActual')).toBe(false)
  })

  it('assigns public boundary metadata only to user-facing account actions', () => {
    expect(getCapability('createLinkToken')?.publicBoundary).toBe('bank-connections')
    expect(getCapability('disconnectInstitution')?.publicBoundary).toBe('bank-connections')
    expect(getCapability('acceptHouseholdInvite')?.publicBoundary).toBe('household-sharing')
    expect(getCapability('updatePrivacySettings')?.publicBoundary).toBe('household-sharing')
    expect(getCapability('refreshRevenueCatSubscription')?.publicBoundary).toBe('subscription-changes')
    expect(getCapability('requestDeletion')?.publicBoundary).toBe('account-deletion')

    expect(getCapability('checkRole')?.publicBoundary).toBeUndefined()
    expect(getCapability('qaAuthBypass')?.publicBoundary).toBeUndefined()
    expect(getCapability('getHealth')?.publicBoundary).toBeUndefined()
    expect(getCapability('updateUserProfile')?.publicBoundary).toBeUndefined()
  })

  it('excludes all admin operations', () => {
    const adminOps = [
      'checkRole',
      'listAdminUsers',
      'getSystemHealth',
      'listAdminDeadLetterJobs',
      'bulkDeleteAdminDeadLetterJobs',
      'get-admin-user-subscription',
      'update-admin-user-tier'
    ]
    for (const opId of adminOps) {
      expect(isExcluded(opId)).toBe(true)
    }
  })

  it('excludes Plaid account-linking operations (permanently non-MCP)', () => {
    expect(isExcluded('exchangePublicToken')).toBe(true)
    expect(isExcluded('createLinkToken')).toBe(true)
    expect(isExcluded('disconnectInstitution')).toBe(true)
    // Issue #1569, Task 6: deleteAccount was renamed/split into disconnectAccount (soft) and
    // purgeAccount (hard delete); disconnectInstitution gained a purgeInstitution sibling. All
    // four stay excluded — an AI assistant must never touch Plaid linking/removal OR a user's
    // financial history.
    expect(isExcluded('disconnectAccount')).toBe(true)
    expect(isExcluded('purgeAccount')).toBe(true)
    expect(isExcluded('purgeInstitution')).toBe(true)
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
      'testBoost'
    ]
    for (const opId of boostWriteOps) {
      expect(isExcluded(opId)).toBe(true)
    }
  })

  it('allows auto-categorization writes with confirmation-gating (Issue #957 Q1)', () => {
    // Rule mutations were un-excluded per Issue #957 Q1 decision.
    // They are confirmation-gated writes, not excluded.
    for (const opId of ['createRule', 'updateRule', 'toggleRule']) {
      expect(isExcluded(opId)).toBe(false)
      expect(getCapability(opId)?.class).toBe('write')
      expect(getCapability(opId)?.confirmationRequired).toBe(true)
    }
    // deleteRule stays invocable under the same #957 decision, but Task 16 moved it up a tier:
    // it is a hard DELETE, unlike createRule/updateRule/toggleRule.
    expect(isExcluded('deleteRule')).toBe(false)
    expect(getCapability('deleteRule')?.class).toBe('destructive')
    expect(getCapability('deleteRule')?.confirmationRequired).toBe(true)
  })

  it('classifies getMerchantOverrides as read (Issue #957 gap fill)', () => {
    const entry = getCapability('getMerchantOverrides')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(false)
  })

  // G40: explicit classification assertions for all 7 merchant ops (Issue #957)
  it('classifies merchant read ops as read, not excluded, no confirmation (Issue #957 G40)', () => {
    for (const opId of ['getMerchants', 'getMerchantDuplicateSuggestions']) {
      const entry = getCapability(opId)
      expect(entry).toBeDefined()
      expect(entry?.class).toBe('read')
      expect(entry?.excluded).toBe(false)
      expect(entry?.confirmationRequired).toBeFalsy()
    }
  })

  it('classifies merchant write ops as confirmation-gated writes (Issue #957 G40)', () => {
    for (const opId of ['setMerchantOverride', 'bulkSetMerchantOverrides', 'mergeMerchants']) {
      const entry = getCapability(opId)
      expect(entry).toBeDefined()
      expect(entry?.class).toBe('write')
      expect(entry?.confirmationRequired).toBe(true)
      expect(entry?.excluded).toBe(false)
      expect(entry?.destructive).toBe(false)
    }
  })

  it('classifies clearMerchantOverride as a confirmation-gated destructive op (#957 G40, #1569 T16)', () => {
    const entry = getCapability('clearMerchantOverride')
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('destructive')
    expect(entry?.confirmationRequired).toBe(true)
    expect(entry?.excluded).toBe(false)
    expect(entry?.destructive).toBe(true)
  })

  it('excludes subscription writes (subscription management stays in-app)', () => {
    expect(isExcluded('create-paddle-checkout')).toBe(true)
    expect(isExcluded('refreshRevenueCatSubscription')).toBe(true)
    expect(isExcluded('beginRevenueCatCheckout')).toBe(true)
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
  it('covers every known operationId (no unknown ops in the spec)', async () => {
    // We import the full list to ensure no ops are silently absent
    // +8 from Issue #138: getHoldings, getPortfolioSummary, getPortfolioHistory,
    //   getInvestmentTransactions, getPortfolioAllocationBreakdown, getTopMovers,
    //   getInvestmentGainLossSummary, getInvestmentIncomeSummary
    // +1 from Issue #1300: getSavingsProgressSummary (basis-aware analytics, Codex C11)
    // +4 from Issue #1300 M20: listSavingsGoalContributions, logSavingsGoalContribution,
    //   confirmSavingsGoalContribution, deleteSavingsGoalContribution
    // +1 from Issue #1300 R3-7: rejectSavingsGoalContribution (sticky tombstone)
    // +7 from Issue #957: getMerchants, getMerchantOverrides, getMerchantDuplicateSuggestions,
    //   setMerchantOverride, clearMerchantOverride, bulkSetMerchantOverrides, mergeMerchants
    // +1 from Issue #1366: getHouseholdEntitlement (Phase 2: household entitlement resolution)
    // +12 from Issue #1367 Phase 3 (Gap Analysis Round 2, GT-4): getMyHousehold,
    //   listHouseholdMembers, listHouseholdInvites, getHouseholdInvitePreview (read);
    //   createHousehold, updateHouseholdName, deleteHousehold, createHouseholdInvite,
    //   revokeHouseholdInvite, removeHouseholdMember, acceptHouseholdInvite,
    //   declineHouseholdInvite (excluded — deliberate in-app action)
    // +2 from Issue #1368 Phase 4: listMyAccountShares (read); setHouseholdShareVisibility
    //   (excluded — privacy/consent decision, deliberate in-app action)
    // +1 from Issue #1413/#1414: remove-admin-user-tier-override (excluded — irreversible,
    //   billing-adjacent admin action)
    // +1 from Issue #1503: listAdminPlaidOrphanedItems (excluded — blanket admin policy)
    // +1 from Issue #1497 item 2: recordAdminPlaidOrphanedItemOperatorResolution (excluded —
    //   blanket admin policy; an operator attestation is an authenticated-human act by definition
    //   and has no meaning coming from an agent, which cannot have removed anything at Plaid)
    // -1 from Issue #1514: getMetrics removed. /metrics moved to an internal-only listener and is
    //   no longer registered for spec generation, so it has no spec-index entry left to classify;
    //   keeping it would have made it a manifest-only entry, invisible to the capabilitySummary
    //   aggregates (see capability-summary.drift.test.ts). It was an *excluded* op, so the
    //   allowed/read/write/destructive counts are unchanged.
    // +2 net from Issue #1569 Task 6: deleteAccount removed; disconnectAccount, purgeAccount, and
    //   purgeInstitution added (disconnectInstitution kept, moved to POST).
    // +3 from Issue #1569 Task 16: purgeSavingsGoal (destructive, excluded), getDeletedSavingsGoals
    //   and getUserBoostExecutions (read). All three were already in api/openapi.yaml but in NO
    //   manifest entry — check:manifest and the spec-index build were both failing at that HEAD.
    // +1 from Issue #1584: listAdminRetentionWithheld (excluded — blanket admin policy; it is a
    //   READ, so the exclusion costs an agent nothing it could act on — the surface exists so a
    //   human operator can pick a remedy for the withheld backlog that does not exist yet).
    // +1 from Issue #1569: getAccountHoldingsHistory (read, invocable — the reader for
    //   holding_snapshots, which was written by every sync and read by nothing until now. It is a
    //   per-resource read scoped to one account and one day, which is why it is not excluded the
    //   way the whole-account GDPR export is; see its entry for the reasoning and the condition
    //   under which that reasoning stops holding).
    // +1 from Issue #1569: getAccountBalanceHistory (read, invocable — the BALANCE twin of the
    //   entry above. balance_snapshots was written by every sync and had no per-account reader at
    //   all: GetLatestByAccountID returned ONE last-known figure and getBalanceHistory has never
    //   taken an account id. Same per-resource shape, same reasoning, same expiry condition.)
    const { ALL_OPERATION_IDS } = await import('./capability-manifest')
    // +2 from Issue #1609 P3-T1: createReconnectLinkToken and repairLinkedItem (both excluded
    //   writes). The exclusion is a CAPABILITY fact, not just policy: repairing a bank connection
    //   requires a human to complete Plaid Link in a browser, so an MCP agent could never carry
    //   either one out. They shipped in the spec before being classified, which broke
    //   check:manifest and the spec-index build until this entry — the same failure mode Task 16
    //   records above.
    // +1 from Issue #1626: getCategoryHierarchy, a read. Every Analytics-tagged operation must
    //   be read-class — TestMCPManifest_AnalyticsOpsAreInGoManifest enforces that on the Go side.
    // +2 from Issue #1628's rule-engine rework (staging): getConditionSchema (read) and
    //   previewRule (write, non-destructive — it returns candidate matches and persists nothing).
    // ⚠ 211 is the SUM of two independent increments from a shared 208, not either side's total:
    //   this branch took it to 209 and staging to 210, and the merge has all three new operations.
    //   Re-derive from `bun run src/spec/index-builder.ts`, which prints the entry count it wrote.
    // +1 from #1630 B7: listAdminBudgetHierarchyAudits, the hierarchy audit's operator surface.
    // +1 from #1637: frontend-only budget plan report, explicitly excluded.
    // +12 from #1652: RevenueCat admin, subscription and webhook surfaces, all excluded.
    // -4 from #1652: the deleted admin trial-grant operation (todo 5) and the three deleted Paddle
    //   self-service routes preview-update / update / cancel (todo 10).
    // +7 from #1650: Support identity, MCP telemetry, and five PostHog erasure operator
    //   endpoints, all internal/admin-only and excluded.
    expect(ALL_OPERATION_IDS.length).toBe(228)
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
  const coreOps = ['getHoldings', 'getPortfolioSummary', 'getPortfolioHistory', 'getInvestmentTransactions']
  const analyticsOps = [
    'getPortfolioAllocationBreakdown',
    'getTopMovers',
    'getInvestmentGainLossSummary',
    'getInvestmentIncomeSummary'
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

// ── Issue #1503: admin Plaid orphaned-items listing ─────────────────────────

describe('issue #1503: listAdminPlaidOrphanedItems', () => {
  it('classifies listAdminPlaidOrphanedItems as excluded under the blanket admin policy', () => {
    const entry = getCapability('listAdminPlaidOrphanedItems')
    expect(entry).toBeDefined()
    expect(entry?.excluded).toBe(true)
    expect(entry?.excludedReason).toBe('Admin endpoints are excluded from MCP')
  })
})

// ── Issue #1569 Task 16: the destructive capability tier ────────────────────
//
// Before this task the manifest had exactly two classes, so an agent granted
// `write` could invoke deleteSavingsGoal indistinguishably from createTag. After
// fifteen tasks on this branch most "delete" verbs tombstone (history retained,
// dependent financial records preserved) while a smaller set genuinely destroys
// rows with no tombstone and no restore path. `destructive` is that smaller set.
//
// Every literal below was verified against backend SQL, not inferred from the verb
// name — see the manifest's own per-operation comments.

describe('issue #1569 task 16: destructive class', () => {
  /**
   * Asserted as a SET (not per-op existence checks) so a newly added destructive verb that
   * nobody classified fails HERE rather than defaulting to `write` and silently becoming
   * agent-invocable. Since Task 16 gave excluded entries their true class, this also catches a
   * newly added EXCLUDED destructive verb — the likelier way a destructive op enters, and the
   * case the pre-Task-16 manifest could not represent at all.
   */
  const DESTRUCTIVE_INVOCABILITY: Record<string, boolean> = {
    // ── INVOCABLE ── hard deletes, but of user-authored CONFIGURATION the agent can read back
    // before deleting and recreate afterwards. None of these touch financial history.
    clearMerchantOverride: true,
    deleteMCPRecipe: true,
    deleteRule: true,
    deleteTag: true,
    // ── EXCLUDED ── destroys financial history, another user's data, or operational records.
    // An agent must never be able to call these; each carries its reason in the manifest.
    bulkDeleteAdminDeadLetterJobs: false,
    deleteAdminDeadLetterJob: false,
    deleteHousehold: false,
    deleteSavingsGoalContribution: false,
    purgeAccount: false,
    purgeInstitution: false,
    purgeSavingsGoal: false
  }

  /**
   * THE SET, derived from the invocability table above rather than hand-maintained beside it —
   * two parallel literal lists over the same operations would drift, and nothing would notice.
   */
  const DESTRUCTIVE_OPERATIONS = Object.keys(DESTRUCTIVE_INVOCABILITY).sort()

  /**
   * Soft-deleting verbs. These tombstone (deleted_at/deleted_reason/deleted_by) and
   * preserve the dependent financial history the old ON DELETE CASCADE destroyed, so
   * they are NOT destructive — that is the whole point of the branch. Pinned so a
   * later change that reverts one to a hard DELETE has to come back through here.
   */
  const SOFT_DELETE_OPERATIONS = ['deleteBudget', 'deleteBudgetCategory', 'deleteCategory', 'deleteSavingsGoal']

  it('pins the exact set of destructive-class operations', () => {
    const actual = ALL_OPERATION_IDS.filter((id) => getCapability(id)?.class === 'destructive').sort()
    expect(actual).toEqual(DESTRUCTIVE_OPERATIONS)
  })

  it('pins invocability per destructive operation', () => {
    for (const [operationId, invocable] of Object.entries(DESTRUCTIVE_INVOCABILITY)) {
      const entry = getCapability(operationId)
      expect(entry, `${operationId} is not in the manifest`).toBeDefined()
      expect(entry?.excluded, `${operationId} excluded flag`).toBe(!invocable)
      expect(isAllowed(operationId), `${operationId} isAllowed`).toBe(invocable)
    }
  })

  it('records an exclusion reason on every excluded destructive operation', () => {
    for (const [operationId, invocable] of Object.entries(DESTRUCTIVE_INVOCABILITY)) {
      if (invocable) continue
      expect(getCapability(operationId)?.excludedReason ?? '', operationId).not.toBe('')
    }
  })

  it('keeps the destructive flag and the destructive class in lockstep for every entry', () => {
    // Two independent fields would drift. The flag IS the class, everywhere.
    const mismatched = ALL_OPERATION_IDS.filter((id) => {
      const e = getCapability(id)!
      return e.destructive !== (e.class === 'destructive')
    })
    expect(mismatched).toEqual([])
  })

  it('records a rationale in the manifest for every destructive-class operation', () => {
    // The brief for this task: say WHY, in the manifest, not only in the report.
    for (const operationId of DESTRUCTIVE_OPERATIONS) {
      const rationale = getCapability(operationId)?.rationale ?? ''
      expect(rationale.length, `${operationId} has no rationale`).toBeGreaterThan(20)
    }
  })

  it('requires confirmation on every destructive-class operation', () => {
    // True even on the excluded ones. The host-proxy throws on `excluded` long before the
    // confirmation gate, so this is never read there; it is set as the fail-safe default for the
    // one tier where being wrong is unrecoverable. excludedWrite() deliberately leaves
    // confirmationRequired at excluded()'s long-standing `false`.
    for (const operationId of DESTRUCTIVE_OPERATIONS) {
      expect(requiresConfirmation(operationId), operationId).toBe(true)
    }
  })

  it('destroysData answers for EXCLUDED destructive ops too, unlike isDestructive', () => {
    // isDestructive() means "available and destroys data" — false for an excluded op, which is
    // the right answer for an invocability question and the wrong one for "would this destroy
    // my data?". destroysData() is the predicate for the latter.
    for (const operationId of DESTRUCTIVE_OPERATIONS) {
      expect(destroysData(operationId), operationId).toBe(true)
    }
    expect(destroysData('purgeAccount')).toBe(true)
    expect(isDestructive('purgeAccount')).toBe(false)
    expect(destroysData('deleteBudget')).toBe(false)
    expect(destroysData('nonExistentOperation')).toBe(false)
  })

  it('gives every excluded entry the same idempotent placeholder', () => {
    // `idempotent` is a placeholder on excluded entries and Task 16 deliberately did not re-guess
    // it (no derivable truth, no consumer — unlike `class`, which the HTTP method settles and two
    // guards now enforce). Pinning the uniformity turns "placeholder" into a stated convention, so
    // a future entry that quietly asserts something about an unreachable operation stands out.
    const odd = ALL_OPERATION_IDS.filter((id) => {
      const e = getCapability(id)!
      return e.excluded && e.idempotent !== true
    })
    expect(odd).toEqual([])
  })

  it('classifies excluded state-changing operations as write, not read', () => {
    // Pre-Task-16 the excluded() helper hard-coded class:'read' onto all 45 excluded mutating
    // operations. These four are status updates / schedulers, not destruction — so 'write' is
    // their true class, and it is now recorded.
    for (const operationId of [
      'deleteBoost',
      'removeHouseholdMember',
      'revokeHouseholdInvite',
      'requestDeletion',
      // Reclassified destructive -> write in round 3. Its DELETE citation pointed at
      // DeleteOverride, a same-named method in an unrelated flow; the real path is an
      // `UPDATE users SET manual_tier_override = NULL` that preserves the prior value in
      // audit_logs.changes. See the manifest entry.
      'remove-admin-user-tier-override'
    ]) {
      const entry = getCapability(operationId)
      expect(entry?.class, operationId).toBe('write')
      expect(entry?.destructive, operationId).toBe(false)
      expect(entry?.excluded, operationId).toBe(true)
    }
  })

  it('classifies soft-deleting verbs as write, not destructive', () => {
    for (const operationId of SOFT_DELETE_OPERATIONS) {
      const entry = getCapability(operationId)
      expect(entry?.class, operationId).toBe('write')
      expect(entry?.destructive, operationId).toBe(false)
      expect(entry?.confirmationRequired, operationId).toBe(true)
      expect(isAllowed(operationId), operationId).toBe(true)
    }
  })

  it('isDestructive is true only for the destructive class and never for excluded ops', () => {
    expect(isDestructive('deleteTag')).toBe(true)
    expect(isDestructive('deleteRule')).toBe(true)
    // Excluded destructive ops are NOT "available and destructive" — they are unavailable.
    expect(isDestructive('purgeSavingsGoal')).toBe(false)
    expect(isDestructive('deleteSavingsGoalContribution')).toBe(false)
    // Soft deletes and plain writes/reads.
    expect(isDestructive('deleteBudget')).toBe(false)
    expect(isDestructive('createTag')).toBe(false)
    expect(isDestructive('getTransactions')).toBe(false)
    expect(isDestructive('nonExistentOperation')).toBe(false)
  })

  it('isWrite excludes the destructive class (destructive is a class above write)', () => {
    // No production caller reads isWrite (verified by grep across the repo at the time
    // of this change); it is asserted here so the narrowing is a decision, not a slip.
    expect(isWrite('deleteTag')).toBe(false)
    expect(isWrite('deleteBudget')).toBe(true)
  })

  it('classifies the three previously-unclassified operations this branch added', () => {
    // These three were in api/openapi.yaml but in NO manifest entry at all, so
    // `bun run check:manifest` and the spec-index build were both failing at HEAD.
    expect(getCapability('purgeSavingsGoal')?.class).toBe('destructive')
    expect(getCapability('getDeletedSavingsGoals')?.class).toBe('read')
    expect(getCapability('getDeletedSavingsGoals')?.excluded).toBe(false)
    expect(getCapability('getUserBoostExecutions')?.class).toBe('read')
    expect(getCapability('getUserBoostExecutions')?.excluded).toBe(false)
  })
})

// ── Issue #1569 T9.4: the automation audit trail stays reachable over MCP ───
//
// `getUserRuleExecutions` (GET /api/v1/rule-executions) is the RULE twin of
// `getUserBoostExecutions`: per-user automation history, tenancy-scoped, read-only, and — the
// point of #1569 — it deliberately returns executions whose rule has since been HARD-deleted,
// carrying rule_name and definition_snapshot in place of the vanished rule_id.
//
// ⚠ WHY AN ASSERTION AND NOT A COMMENT. This manifest is DENY-BY-DEFAULT: `getCapability` returns
// undefined for anything unlisted and `isAllowed` is then false. So the operation losing its entry
// — a merge that drops a line, a reshuffle of the Boosts block, a future "tighten MCP" sweep —
// silently removes the assistant's only route to the retained trail, with no test anywhere failing
// and no error at the call site. Reclassifying it as `excluded` does the same thing more quietly
// still. The user's ruling for this task was "build the UI, and ensure this is exposed via mcp";
// the MCP half was already true, so what this task owes is the guard that keeps it true.
//
// This test is deliberately whole-shape rather than a single `class` check: `excluded` and
// `confirmationRequired` are each independently able to make a read operation unreachable or
// unusable in a tool loop.
describe('issue #1569 t9.4: getUserRuleExecutions stays an invocable read', () => {
  it('is classified, read, not excluded, and needs no confirmation', () => {
    const entry = getCapability('getUserRuleExecutions')
    // Deny-by-default: undefined here means the manifest entry was removed outright.
    expect(entry).toBeDefined()
    expect(entry?.class).toBe('read')
    expect(entry?.excluded).toBe(false)
    expect(entry?.confirmationRequired).toBe(false)
  })

  it('is actually invocable, not merely present', () => {
    expect(isAllowed('getUserRuleExecutions')).toBe(true)
    expect(isExcluded('getUserRuleExecutions')).toBe(false)
    // A read is never a write and never destructive — reclassifying upward would gate the audit
    // trail behind a confirmation the assistant cannot give on a read.
    expect(isWrite('getUserRuleExecutions')).toBe(false)
    expect(destroysData('getUserRuleExecutions')).toBe(false)
  })

  // Deliberately NOT asserted here: that the operation exists in the published spec index.
  // `ALL_OPERATION_IDS` is derived from MANIFEST_ENTRIES, so asserting membership in it would only
  // restate `expect(entry).toBeDefined()` above while LOOKING like a spec check. The real guard
  // already exists and is generic — capability-summary.drift.test.ts, "every manifest entry has a
  // corresponding spec-index entry" — and it covers this operation with no per-op line needed.
})
