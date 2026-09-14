/**
 * Drift test for the public /mcp page's capability summary (Issue #1411).
 *
 * apps/web/src/features/mcp/data/capabilitySummary.json is a COMMITTED,
 * generated file (via scripts/generate-mcp-capability-summary.ts) — apps/web
 * has no build step of its own that regenerates it, and must never import
 * this package's manifest or spec index directly. This test is the tripwire:
 * it recomputes the aggregates fresh from the manifest + spec index and fails
 * if the committed JSON has drifted, so a manifest change can't silently make
 * the marketing page's numbers wrong.
 *
 * Known values at the time this page shipped (verified against the manifest,
 * not trusted from the issue brief): 194 total, 133 allowed, 87 read, 46 write,
 * 61 excluded, 9 destructive writes (#1503 added listAdminPlaidOrphanedItems as the
 * 61st exclusion). Asserted as documentation below — expect
 * this test (not those literals) to be the source of truth as the manifest grows.
 * ⚠ WAS 195 total / 62 excluded: Issue #1497 item 2 added
 * recordAdminPlaidOrphanedItemOperatorResolution, excluded under the blanket admin policy. The
 * allowed/read/write/destructive figures are unchanged, which is the point of the exclusion.
 * ⚠ WAS 194 total / 61 excluded: Issue #1514 moved /metrics to an internal-only listener and
 * stopped registering it for spec generation, so getMetrics no longer has a spec-index entry and
 * its (excluded) manifest classification was removed. Again allowed/read/write/destructive are
 * unchanged — only an exclusion disappeared, so the public-facing "what can the AI do" numbers
 * on the /mcp page are unaffected.
 * ⚠ NOW 196 total / 63 excluded: Issue #1569 Task 6 split the single deleteAccount /
 * disconnectInstitution DELETE-backed verb into disconnect (non-destructive) and purge
 * (destructive) for both accounts and linked items — net +2 operationIds (deleteAccount removed,
 * disconnectAccount/purgeAccount/purgeInstitution added; disconnectInstitution kept, moved to
 * POST) and net +2 exclusions, since all four new/renamed operations stay excluded under
 * REASON_PLAID_LINKING. allowed/read/write/destructive are unchanged.
 * ⚠ NOW 199 total / 134 allowed / 89 read / 45 write / 65 excluded / 4 destructive: Issue #1569
 * Task 16 (a) classified three operations that were in api/openapi.yaml but in NO manifest entry
 * at all — purgeSavingsGoal (destructive, excluded), getDeletedSavingsGoals and
 * getUserBoostExecutions (read), which is +3 total, +2 read, +1 excluded; (b) excluded
 * deleteSavingsGoalContribution, which was an allowed write that hard-deletes a savings ledger
 * row (−1 allowed write, +1 excluded); and (c) introduced the `destructive` class. Note what
 * (c) did NOT do to these numbers: writeOperations still counts every allowed MUTATING
 * operation (class 'write' OR 'destructive'), so allowed === read + write still holds and the
 * public page's sentence stays arithmetically true. destructiveWriteOperations fell 9 → 4
 * because deleteBudget/deleteBudgetCategory/deleteCategory/deleteSavingsGoal now tombstone
 * rather than destroy, and deleteSavingsGoalContribution left the allowed set entirely.
 * ⚠ NOW 204 total / 67 excluded: Issue #1584 added listAdminRetentionWithheld, the enumerating
 * admin read over accounts the #1569 T1 pre-deletion-notice gate is holding indefinitely. Excluded
 * under the blanket admin policy, so allowed/read/write/destructive are all unchanged — the public
 * /mcp page's "what can the AI do" numbers are unaffected, which is the point of the exclusion.
 */

import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { computeCapabilitySummary, readSpecIndex } from '../../scripts/generate-mcp-capability-summary'
import { ALL_OPERATION_IDS, getCapability } from './capability-manifest'

const __dirname = dirname(fileURLToPath(import.meta.url))
const COMMITTED_JSON_PATH = resolve(__dirname, '../../../web/src/features/mcp/data/capabilitySummary.json')

describe('capability summary drift (apps/web/src/features/mcp/data/capabilitySummary.json)', () => {
  it('matches the aggregates freshly computed from the manifest + spec index', () => {
    const fresh = computeCapabilitySummary(readSpecIndex())
    const committedRaw = readFileSync(COMMITTED_JSON_PATH, 'utf-8')
    const committed = JSON.parse(committedRaw)

    expect(
      committed,
      'apps/web/src/features/mcp/data/capabilitySummary.json is stale — ' +
        'regenerate with: bun run scripts/generate-mcp-capability-summary.ts (from apps/mcp-server)'
    ).toEqual(fresh)
  })

  it('contains only curated public boundaries, never internal exclusion reasons', () => {
    const committedRaw = readFileSync(COMMITTED_JSON_PATH, 'utf-8')
    const committed = JSON.parse(committedRaw)

    expect(committed.exclusionGroups).toBeUndefined()
    expect(committed.readCountByTag).toBeUndefined()
    expect(committed.publicBoundaries).toEqual([
      {
        id: 'bank-connections',
        title: 'Bank connections',
        description: 'Linking or disconnecting a bank connection stays in BoostedBudget.'
      },
      {
        id: 'household-sharing',
        title: 'Household membership and sharing',
        description: 'Invites, membership changes, sharing, and consent stay in BoostedBudget.'
      },
      {
        id: 'subscription-changes',
        title: 'Subscription changes',
        description: 'Starting, changing, or cancelling a subscription stays in BoostedBudget.'
      },
      {
        id: 'account-deletion',
        title: 'Account deletion',
        description: 'Requesting or cancelling BoostedBudget account deletion stays in BoostedBudget.'
      }
    ])
    expect(committedRaw).not.toMatch(/Admin endpoints|QA\/testing|Internal service|follow-up issue/i)
  })

  it('documents the known baseline values (informational — the drift check above is authoritative)', () => {
    const fresh = computeCapabilitySummary(readSpecIndex())

    // +1 across totals/allowed/read from Issue #1569: getAccountBalanceHistory, the per-account
    // retained balance history reader (read, invocable). write/excluded/destructive are unchanged
    // — it mutates nothing and destroys nothing.
    // +2 across totals/excluded from Issue #1609 P3-T1: createReconnectLinkToken and
    // repairLinkedItem. Both are excludedWrite, so `allowed`/`read`/`write` are UNCHANGED and only
    // total and excluded move — reconnecting a bank needs a human in Plaid Link, so neither is a
    // capability an agent could ever exercise.
    // #1626 added getCategoryHierarchy, a read: +1 total, +1 allowed, +1 read.
    // #1628's rule-engine rework (staging) added getConditionSchema and previewRule, BOTH
    // read-class (previewRule is a dry run — it returns candidate matches and persists nothing):
    // +2 total, +2 allowed, +2 read.
    // ⚠ These three additions are INDEPENDENT and both sides' numbers were correct for their own
    // branch (209/140/95 here, 210/141/96 on staging, from a shared 208/139/94). The merged totals
    // are the sum: 211/142/97. `write`, `excluded` and `destructive` are untouched by all three,
    // which is what the two closure assertions below re-derive. The generators are the authority —
    // `bun run scripts/generate-mcp-capability-summary.ts` prints exactly this breakdown.
    // #1630 B7 added listAdminBudgetHierarchyAudits — a GET, but EXCLUDED under the blanket admin
    // policy, so it moves total and excluded only: 211 -> 212 and 69 -> 70. allowed/read/write are
    // deliberately unchanged, which is what distinguishes an excluded addition from an allowed one.
    // #1637 adds one excluded frontend-only report; exposed capabilities stay unchanged.
    // #1650 adds the excluded Support identity and MCP telemetry infrastructure endpoints.
    expect(fresh.totalOperations).toBe(215)
    expect(fresh.allowedOperations).toBe(142)
    expect(fresh.readOperations).toBe(97)
    expect(fresh.writeOperations).toBe(45)
    expect(fresh.excludedOperations).toBe(73)
    expect(fresh.destructiveWriteOperations).toBe(4)
    expect(fresh.allowedOperations).toBe(fresh.readOperations + fresh.writeOperations)
    expect(fresh.totalOperations).toBe(fresh.allowedOperations + fresh.excludedOperations)

    expect(fresh.publicBoundaries).toHaveLength(4)
  })

  /**
   * #1569 Task 16. The drift test above compares AGGREGATES, so two classification changes
   * that cancel out (one op write→destructive, another destructive→write) would leave every
   * count identical and this file green. This pins the per-operation class carried in the
   * committed spec index against the manifest, so any single reclassification is caught even
   * when the totals do not move.
   */
  it('the committed spec index agrees with the manifest on every operation class', () => {
    const disagreements = readSpecIndex()
      .entries.filter((e) => {
        const capability = getCapability(e.operationId)
        return !capability || capability.class !== e.manifestClass || capability.excluded !== e.excluded
      })
      .map((e) => e.operationId)

    expect(
      disagreements,
      'spec-index.json is stale relative to the manifest — rebuild with: bun run build:spec-index'
    ).toEqual([])
  })

  /**
   * The destructive tier is a public-facing claim ("N of these permanently destroy data"), so
   * pin its membership from the summary's own inputs rather than trusting the aggregate count.
   */
  it('counts exactly the allowed destructive-class operations in destructiveWriteOperations', () => {
    const allowedDestructive = readSpecIndex()
      .entries.filter((e) => !e.excluded && e.manifestClass === 'destructive')
      .map((e) => e.operationId)
      .sort()

    expect(allowedDestructive).toEqual(['clearMerchantOverride', 'deleteMCPRecipe', 'deleteRule', 'deleteTag'])
    expect(computeCapabilitySummary(readSpecIndex()).destructiveWriteOperations).toBe(allowedDestructive.length)
  })

  /**
   * #1569 Task 16, F1. The `excluded()` helper hard-coded `class: 'read'` onto every excluded
   * entry, so the manifest asserted that purgeAccount, deleteHousehold and 43 other mutating
   * verbs neither change nor destroy anything. Hand-fixing those 45 entries would only hold
   * until the next one is added, so these two guards derive the invariant from the spec index's
   * own HTTP methods and hold automatically for every future operation.
   */
  const NON_MUTATING_POSTS = new Set([
    'previewCategoryMatches', // POST /budgets/categories/preview — returns matches, writes nothing
    // #1625: POST /auto-categorization/preview — a dry run of a CANDIDATE rule. It persists
    // nothing, bumps no match count and writes no execution history; the service is exercised
    // with a nil rule repository in TestPreviewRule_HasNoWriteSideEffects, so a write would
    // nil-panic rather than pass. Same shape as previewCategoryMatches above.
    'previewRule',
    'runMCPRecipe' // v1 recipes compose read-class ops only (see the manifest's own note)
  ])

  it('classifies every GET operation as read', () => {
    const offenders = readSpecIndex()
      .entries.filter((e) => e.method === 'GET' && e.manifestClass !== 'read')
      .map((e) => `${e.operationId} (${e.manifestClass})`)
    expect(offenders).toEqual([])
  })

  it('classifies every state-changing operation as write or destructive, excluded ones included', () => {
    const offenders = readSpecIndex()
      .entries.filter(
        (e) =>
          e.method !== 'GET' &&
          e.manifestClass === 'read' &&
          !NON_MUTATING_POSTS.has(e.operationId)
      )
      .map((e) => `${e.method} ${e.operationId}`)

    expect(
      offenders,
      'These operations change state but are classified `read`. If the operation really is ' +
        'non-mutating, add it to NON_MUTATING_POSTS with a reason; otherwise use write(), ' +
        'excludedWrite(), or destructive() in capability-manifest.ts.'
    ).toEqual([])
  })

  it('committed JSON ends with a trailing newline (matches the generator script)', () => {
    const raw = readFileSync(COMMITTED_JSON_PATH, 'utf-8')
    expect(raw.endsWith('\n')).toBe(true)
  })

  // Codex review finding #8: computeCapabilitySummary() iterates the spec index, so a
  // manifest entry with no corresponding spec-index entry (e.g. added to the manifest but
  // not yet wired to an OpenAPI operation) is silently invisible to it — the drift check
  // above would stay green even though the manifest and the summary have diverged. This
  // guards that gap directly: every manifest entry must have a spec-index counterpart.
  it('every manifest entry has a corresponding spec-index entry (catches manifest-only additions)', () => {
    const specIndex = readSpecIndex()
    const specOperationIds = new Set(specIndex.entries.map((e) => e.operationId))

    const manifestOnly = ALL_OPERATION_IDS.filter((id) => !specOperationIds.has(id))

    expect(
      manifestOnly,
      'These operationIds are classified in capability-manifest.ts but have no spec-index ' +
        'entry, so they are invisible to the capabilitySummary.json aggregates above. Either ' +
        'they are stale manifest entries (remove them) or the spec index needs rebuilding ' +
        '(bun run build:spec-index).'
    ).toEqual([])

    // And the reverse count check the summary itself relies on: no spec-index entry should
    // silently outnumber the manifest (would mean an unclassified operation slipped through —
    // scripts/check-manifest-completeness.ts is the primary guard for this, this is a
    // cross-check from the drift test's own inputs).
    expect(specIndex.entries.length).toBeLessThanOrEqual(ALL_OPERATION_IDS.length)
  })
})
