/**
 * Budget Vs Actual — Nested Custom Category Containment (#1630)
 *
 * Round 2 of #1630 fixed a real double-count: GetBudgetVsActual (and the four frontend allocation
 * sums) used to sum every budget_categories row unconditionally, so a custom category nested
 * under a budgeted PFC parent got counted TWICE — once via its own row, and again inside the
 * parent's own spent_amount, which already rolls up its descendants (Issue #1203). The fix
 * (557044b1) added a per-row dedup keyed on domain.BudgetCategory.IsSubAllocationWithin /
 * match_logic containment.
 *
 * That fix has real Postgres integration coverage
 * (TestGetBudgetVsActual_Integration_DedupSubAllocations) but zero E2E coverage — every existing
 * budget spec (budget-hierarchy.auth.spec.ts) mocks `page.route`, so it never exercises the actual
 * SQL this defect lived in. This spec goes through the REAL backend: a real budget, a real custom
 * category, a real transaction category override, and a real POST /sync recompute — no mocks.
 *
 * Strategy: rather than asserting an absolute dollar total (fragile against whatever other
 * FOOD_AND_DRINK spend a shared seed/CI run happens to carry), this isolates the ONE variable the
 * defect turns on: adding the nested child budget line must not change the budget's total
 * spent_amount. Before/after the nested child is added, the parent's own hierarchical roll-up
 * already includes the reassigned transaction's money either way (#1203); a working dedup keeps
 * the totals equal, and the double-count this task guards against would make them differ.
 */

import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { apiClient } from '../helpers/api/client'

const RUN_ID = Date.now()
const BUDGET_NAME = `E2E Containment Budget ${RUN_ID}`
const CUSTOM_CATEGORY_NAME = `E2E Containment Category ${RUN_ID}`

interface BudgetVsActualItem {
  budget_id: string
  budget_name: string
  spent_amount: number
  allocated_amount: number
}

async function getBudgetVsActualItem(
  client: Awaited<ReturnType<typeof apiClient>>,
  budgetId: string
): Promise<BudgetVsActualItem> {
  const res = await client.get('/api/v1/analytics/budget-vs-actual?period=monthly')
  expect(res.ok(), `GET budget-vs-actual failed: ${await res.text()}`).toBe(true)
  const body: { budgets: BudgetVsActualItem[] | null } = await res.json()
  const item = (body.budgets ?? []).find((b) => b.budget_id === budgetId)
  expect(item, `budget ${budgetId} missing from budget-vs-actual response`).toBeTruthy()
  return item!
}

async function syncBudget(client: Awaited<ReturnType<typeof apiClient>>, budgetId: string) {
  const res = await client.post(`/api/v1/budgets/${budgetId}/sync`, {})
  expect(res.ok(), `POST /budgets/${budgetId}/sync failed: ${await res.text()}`).toBe(true)
}

test.describe('Budget vs Actual — nested custom category containment (#1630)', () => {
  let budgetId: string | undefined
  let customCategoryId: string | undefined
  let starbucksTxnId: string | undefined
  const ORIGINAL_STARBUCKS_CATEGORY = { kind: 'pfc' as const, pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_COFFEE' }

  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test.afterEach(async ({ page }) => {
    // Best-effort cleanup so this leaves no state behind for other specs or CI retries — mirrors
    // cache-freshness.auth.spec.ts's own non-fatal cleanup pattern.
    const client = await apiClient(page).catch(() => null)
    if (!client) return
    if (starbucksTxnId) {
      await client.put(`/api/v1/transactions/${starbucksTxnId}/category`, ORIGINAL_STARBUCKS_CATEGORY).catch(() => {})
    }
    if (budgetId) {
      await client.delete(`/api/v1/budgets/${budgetId}`).catch(() => {})
    }
    if (customCategoryId) {
      await client.delete(`/api/v1/categories/${customCategoryId}`).catch(() => {})
    }
  })

  test('adding a nested custom-category child does not double the budget total', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')
    const client = await apiClient(page)

    // ── ARRANGE: find the real seeded "Starbucks" transaction ($6.50, FOOD_AND_DRINK_COFFEE) ──
    // Chosen deliberately over the seed's other FOOD_AND_DRINK row ("Chipotle Mexican Grill",
    // $45.00): cache-freshness.auth.spec.ts and cache-household-visibility.auth.spec.ts both
    // move/revert that SAME Chipotle row against real Postgres, and interleaving with either
    // would corrupt this spec's before/after comparison. Starbucks is untouched by any
    // live-backend spec (grep-verified).
    const txnRes = await client.get('/api/v1/transactions?search=Starbucks&limit=10')
    expect(txnRes.ok(), `GET transactions search failed: ${await txnRes.text()}`).toBe(true)
    const txnBody: { transactions: Array<{ id: string; amount: number; pfc_primary?: string }> } =
      await txnRes.json()
    const starbucks = txnBody.transactions.find((t) => Math.abs(t.amount - 6.5) < 0.001)
    expect(starbucks, 'seeded Starbucks ($6.50) transaction not found — run scripts/seed-e2e-data.sql').toBeTruthy()
    starbucksTxnId = starbucks!.id

    // ── ARRANGE: a dedicated budget + a custom category nested under FOOD_AND_DRINK ──
    const now = new Date()
    const startDate = new Date(now.getFullYear(), now.getMonth(), 1).toISOString().split('T')[0]
    const createBudgetRes = await client.post('/api/v1/budgets', {
      name: BUDGET_NAME,
      period: 'monthly',
      start_date: `${startDate}T00:00:00Z`,
      amount: 500,
      expected_income: 5000,
      currency_code: 'USD',
    })
    expect(createBudgetRes.ok(), `create budget failed: ${await createBudgetRes.text()}`).toBe(true)
    const createdBudget = await createBudgetRes.json()
    budgetId = (createdBudget.budget ?? createdBudget).id
    expect(budgetId).toBeTruthy()

    const createCategoryRes = await client.post('/api/v1/categories', {
      name: CUSTOM_CATEGORY_NAME,
      parent_pfc_primary: 'FOOD_AND_DRINK',
    })
    expect(createCategoryRes.ok(), `create custom category failed: ${await createCategoryRes.text()}`).toBe(true)
    const createdCategory = await createCategoryRes.json()
    customCategoryId = createdCategory.id
    expect(customCategoryId).toBeTruthy()

    // Reassign Starbucks under the new custom category. Its money now rolls up into
    // FOOD_AND_DRINK's hierarchical parent total via the custom category's own
    // parent_pfc_primary, exactly the "nested child" shape #1630 fixed.
    const reassignRes = await client.put(`/api/v1/transactions/${starbucksTxnId}/category`, {
      kind: 'custom',
      custom_category_id: customCategoryId,
    })
    expect(reassignRes.ok(), `reassign Starbucks failed: ${await reassignRes.text()}`).toBe(true)

    // Add ONLY the parent PFC budget line for now (no nested child budget line yet).
    const addParentRes = await client.post(`/api/v1/budgets/${budgetId}/categories`, {
      category_kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      allocated_amount: 300,
      match_logic: 'category_only',
    })
    expect(addParentRes.ok(), `add parent category failed: ${await addParentRes.text()}`).toBe(true)

    await syncBudget(client, budgetId!)
    const before = await getBudgetVsActualItem(client, budgetId!)

    // Sanity: the reassigned transaction's money must actually be counted, or a dedup bug that
    // zeroes everything would pass this spec vacuously.
    expect(before.spent_amount, 'parent FOOD_AND_DRINK spent_amount should already include the reassigned $6.50 Starbucks transaction via hierarchical roll-up').toBeGreaterThanOrEqual(6.5)

    // ── ACT: add the NESTED CHILD budget line for the same custom category ──
    const addChildRes = await client.post(`/api/v1/budgets/${budgetId}/categories`, {
      category_kind: 'custom',
      custom_category_id: customCategoryId,
      allocated_amount: 50,
      match_logic: 'category_only',
    })
    expect(addChildRes.ok(), `add nested child category failed: ${await addChildRes.text()}`).toBe(true)

    await syncBudget(client, budgetId!)
    const after = await getBudgetVsActualItem(client, budgetId!)

    // ── ASSERT: the total did not double (or otherwise move) once the nested child row exists ──
    expect(
      after.spent_amount,
      `budget total spent_amount changed from ${before.spent_amount} to ${after.spent_amount} after ` +
        'adding a nested custom-category child — the same transaction is being counted twice ' +
        "(once via the child's own row, once via the parent's hierarchical roll-up), which is " +
        'exactly the #1630 defect GetBudgetVsActual\'s per-row dedup exists to prevent.'
    ).toBeCloseTo(before.spent_amount, 2)

    expect(
      after.allocated_amount,
      'the nested child\'s allocated_amount must be excluded from the budget total too — only ' +
        "the parent's own allocated_amount should count"
    ).toBeCloseTo(before.allocated_amount, 2)
  })
})
