/**
 * Retained-history E2E — Issue #1569.
 *
 * WHY THIS FILE EXISTS. This branch's whole argument is that retained financial history is worth
 * nothing if the user cannot SEE it. Every other level of testing on the branch is strong; the
 * browser level was empty — not one of the 48 existing specs opened any of the four retained-history
 * surfaces. These tests are the only evidence that the retention reaches a real rendered page.
 *
 * THE TWO SURFACES COVERED, and why these two:
 *
 *   1. The deleted-goal contribution ledger (/savings → DeletedGoalsPanel → DeletedGoalContributions).
 *      Its absence was the Critical on this branch, and it is the only retained-history surface with
 *      a DESTRUCTIVE verb behind it (POST /savings-goals/{id}/purge). It also shows the one class of
 *      money no provider can ever supply again: contributions the user typed in by hand.
 *   2. Closed-account balance history (/accounts/:id → ClosedAccountBalanceHistory). This is the
 *      clause privacy policy §8.2a promises in words — a removed account's "balance ... history is
 *      retained ... so you can still view it" — and the BALANCE third of that sentence was false
 *      until this branch. A live account is its negative control.
 *
 * ⚠ WHY A NEW FILE RATHER THAN savings.auth.spec.ts. That spec deliberately stubs
 * GET /savings-goals/deleted to `[]` (see its `isDeletedGoalsRequest`), because its
 * `**\/api/v1/savings-goals**` glob would otherwise serve its LIVE goal fixtures back as a list of
 * DELETED goals. That stub is correct and stays. This file serves a populated deleted listing from
 * its own route setup instead.
 *
 * ⚠ RUNS LOCALLY ONLY TODAY. The E2E job on this branch is cancelled at the 35-minute CI timeout on
 * every run, so nothing here is enforced by CI yet. That is a reason to keep the added runtime small
 * (four tests), not a reason to skip it.
 *
 * All fixtures are served by page.route() — this suite's established pattern — so nothing depends on
 * seeded DB state and no destructive verb is ever actually executed against a shared database.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'
import { acceptCookieConsent } from './helpers/auth'

// ---------------------------------------------------------------------------
// Surface 1 fixtures — a deleted savings goal and the contributions kept under it
// ---------------------------------------------------------------------------

const DELETED_GOAL = {
  id: 'goal-deleted-e2e-1',
  user_id: 'user-123',
  name: 'Kyoto Trip',
  description: null,
  target_amount: 4000,
  current_amount: 900,
  currency_code: 'USD',
  account_id: 'acc-1',
  deadline: null,
  is_completed: false,
  completed_at: null,
  // `balance` is deliberately not `contributions`: it is the case where the goal's own "saved"
  // figure legitimately differs from the ledger total, which is exactly what the reconciliation
  // sentence under the ledger exists to explain.
  progress_basis: 'balance',
  created_at: '2025-11-01T00:00:00Z',
  updated_at: '2026-03-20T12:00:00Z',
  // Midday UTC, not midnight: `formatDeletedOn` renders in the VIEWER's timezone, and a midnight-UTC
  // instant renders as the previous day anywhere west of Greenwich. Same reason `formatHistoryLabel`
  // pins its own parse to T12:00:00Z.
  //
  // ⚠ That fixture choice alone is NOT what makes the "March 20, 2026" assertion below hermetic —
  // midday UTC still renders as March 21 in Pacific/Kiritimati. The environment is pinned too:
  // playwright.config.ts sets `timezoneId: 'UTC'` and `locale: 'en-US'` for every project
  // (#1569 T8-FIX, finding F). If either pin is removed, this assertion fails on a valid runner.
  deleted_at: '2026-03-20T12:00:00Z',
}

const LIVE_GOAL = {
  ...DELETED_GOAL,
  id: 'goal-live-e2e-1',
  name: 'Emergency Fund',
  current_amount: 2000,
  deleted_at: undefined,
}

const RETAINED_CONTRIBUTIONS = {
  // 250 + 100 confirmed; the proposed 40 is retained and listed but excluded from the total.
  total_confirmed: 350,
  contributions: [
    {
      id: 'contrib-1',
      savings_goal_id: DELETED_GOAL.id,
      amount: 250,
      occurred_on: '2026-03-15T00:00:00Z',
      source: 'manual',
      status: 'confirmed',
      confirmed_by_user: true,
      managed_by_reconciliation: false,
      created_at: '2026-03-15T00:00:00Z',
      updated_at: '2026-03-15T00:00:00Z',
    },
    {
      id: 'contrib-2',
      savings_goal_id: DELETED_GOAL.id,
      amount: 100,
      occurred_on: '2026-02-01T00:00:00Z',
      source: 'transaction_match',
      status: 'confirmed',
      confirmed_by_user: true,
      managed_by_reconciliation: true,
      created_at: '2026-02-01T00:00:00Z',
      updated_at: '2026-02-01T00:00:00Z',
    },
    {
      id: 'contrib-3',
      savings_goal_id: DELETED_GOAL.id,
      amount: 40,
      occurred_on: '2026-01-05T00:00:00Z',
      source: 'transaction_match',
      status: 'proposed',
      confirmed_by_user: false,
      managed_by_reconciliation: true,
      created_at: '2026-01-05T00:00:00Z',
      updated_at: '2026-01-05T00:00:00Z',
    },
  ],
}

function progressSummaryFor(goals: Array<{ id: string }>) {
  return {
    goals: goals.map((g) => ({
      goal_id: g.id,
      goal_name: 'x',
      current_amount: 0,
      target_amount: 1,
      percent_complete: 0,
      progress_basis: 'balance',
      computed_from: 'account_balance',
      confirmed_contributions_total: 0,
      freshness: 'fresh',
    })),
    total_saved: 0,
    total_target: 1,
    overall_percent_complete: 0,
    goal_count: goals.length,
    completed_goal_count: 0,
  }
}

/**
 * One handler for the whole `**\/api/v1/savings-goals**` glob, dispatched on pathname.
 *
 * ⚠ Not four separate page.route() calls: the glob matches /savings-goals, /savings-goals/deleted
 * AND /savings-goals/deleted/{id}/contributions, so overlapping handlers would answer each other's
 * requests. This is the same reason savings.auth.spec.ts dispatches inside one handler.
 */
async function mockSavingsWithDeletedGoal(page: Page) {
  await page.route('**/api/v1/savings-goals**', (route) => {
    const pathname = new URL(route.request().url()).pathname

    if (pathname.endsWith(`/deleted/${DELETED_GOAL.id}/contributions`)) {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(RETAINED_CONTRIBUTIONS),
      })
      return
    }
    if (pathname.endsWith('/savings-goals/deleted')) {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([DELETED_GOAL]),
      })
      return
    }
    if (pathname.endsWith('/savings-goals/progress-summary')) {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(progressSummaryFor([LIVE_GOAL])),
      })
      return
    }
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([LIVE_GOAL]),
      })
      return
    }
    route.continue()
  })

  await page.route('**/api/v1/accounts*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: [] }),
    })
  )
}

// ---------------------------------------------------------------------------
// Surface 2 fixtures — a CLOSED account with retained day-by-day balances
// ---------------------------------------------------------------------------

const CLOSED_ACCOUNT = {
  id: 'acc-closed-e2e-1',
  user_id: 'user-123',
  linked_item_id: 'item-closed',
  linked_item_status: 'active',
  name: 'Old Savings',
  official_name: null,
  nickname: null,
  mask: '9911',
  type: 'depository',
  subtype: 'savings',
  institution_name: 'Ally',
  // A terminal account presents NULL, not zero, for its current balance — the whole point of the
  // two-universes rule. The retained figures live in the balance-history series below.
  current_balance: null,
  available_balance: null,
  currency_code: 'USD',
  is_hidden: false,
  display_order: 0,
  custom_color: null,
  custom_emoji: null,
  custom_icon: null,
  status: 'removed',
  removed_at: '2026-04-10T00:00:00Z',
  removed_reason: 'user_disconnect',
  plaid_account_id: 'plaid-closed-1',
  replaced_by_account_id: null,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2026-04-10T00:00:00Z',
}

const LIVE_ACCOUNT = {
  ...CLOSED_ACCOUNT,
  id: 'acc-live-e2e-1',
  name: 'Everyday Checking',
  mask: '0042',
  subtype: 'checking',
  institution_name: 'Chase',
  current_balance: 2500,
  available_balance: 2300,
  status: 'active',
  removed_at: null,
  removed_reason: null,
  plaid_account_id: 'plaid-live-1',
}

const RETAINED_BALANCES = {
  account_id: CLOSED_ACCOUNT.id,
  first_date: '2026-04-08',
  last_date: '2026-04-10',
  points: [
    // Every figure distinct, so an assertion on one column can never be satisfied by the other.
    { date: '2026-04-08', current_balance: 1200.5, available_balance: 1150.25, currency_code: 'USD' },
    { date: '2026-04-09', current_balance: 1310.25, available_balance: 1300.75, currency_code: 'USD' },
    // A day the provider reported no available figure: it must render as an em dash, never as $0.00.
    { date: '2026-04-10', current_balance: 980, currency_code: 'USD' },
  ],
}

async function mockClosedAccountHistory(page: Page) {
  await page.route('**/api/v1/accounts**', (route) => {
    const pathname = new URL(route.request().url()).pathname

    if (pathname.endsWith(`/${CLOSED_ACCOUNT.id}/balance-history`)) {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(RETAINED_BALANCES),
      })
      return
    }
    // Any other account's balance-history: an empty series, so a stray read cannot borrow the
    // closed account's rows and make a negative-control assertion pass for the wrong reason.
    if (pathname.endsWith('/balance-history')) {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ account_id: 'unknown', points: [] }),
      })
      return
    }
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: [CLOSED_ACCOUNT, LIVE_ACCOUNT] }),
    })
  })

  await page.route('**/api/v1/investments/holdings**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ count: 0, holdings: [] }),
    })
  )
}

// ---------------------------------------------------------------------------
// Surface 1 — the deleted-goal contribution ledger
// ---------------------------------------------------------------------------

test.describe('Retained history — deleted savings goal ledger (#1569)', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
    await mockAuthDependencies(page)
    await mockSavingsWithDeletedGoal(page)
  })

  test('a deleted goal is listed as kept, and its contributions are readable with amounts and dates', async ({
    page,
  }) => {
    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    // The panel names the state without implying destruction, and counts what was kept.
    const panelToggle = page.getByRole('button', { name: /deleted goals \(1\)/i })
    await expect(panelToggle).toBeVisible({ timeout: 15000 })
    await expect(panelToggle).toHaveAttribute('aria-expanded', 'false')
    await panelToggle.click()

    // The deleted goal's own row: its name, when it was deleted, and its frozen progress.
    await expect(page.getByText('Kyoto Trip')).toBeVisible()
    await expect(page.getByText(/Deleted on March 20, 2026/)).toBeVisible()
    // Whole dollars: the panel header uses `formatCurrency` (0 decimals), not `formatCurrencyFull`.
    await expect(page.getByText(/\$900 of \$4,000/)).toBeVisible()

    // The live goal must NOT have been swept into the deleted listing — the failure mode the
    // savings.auth.spec.ts stub exists to prevent, asserted here against a populated listing.
    await expect(page.getByText(/Deleted on /)).toHaveCount(1)

    // The ledger is collapsed until asked for, so a collapsed row costs no request.
    await expect(page.getByText('Confirmed total')).toHaveCount(0)

    await page.getByRole('button', { name: /contributions: kyoto trip/i }).click()

    // Every retained row is visible, with its real amount and its real date — including the
    // hand-entered one, which no provider could ever supply again.
    await expect(page.getByText('Entered by hand')).toBeVisible()
    await expect(page.getByText('Mar 15, 2026')).toBeVisible()
    await expect(page.getByText('$250.00')).toBeVisible()

    await expect(page.getByText('Detected transfer')).toBeVisible()
    await expect(page.getByText('Feb 1, 2026')).toBeVisible()
    await expect(page.getByText('$100.00')).toBeVisible()

    // A proposed row is retained and shown, but excluded from the confirmed total.
    await expect(page.getByText('Proposed')).toBeVisible()
    await expect(page.getByText('$40.00')).toBeVisible()
    await expect(page.getByText('Confirmed total')).toBeVisible()
    await expect(page.getByText('$350.00')).toBeVisible()

    // The two figures on screen are reconciled in words rather than left looking like an error.
    await expect(
      page.getByText(/last recorded progress, tracked from Account Balance, frozen when you deleted it/)
    ).toBeVisible()
  })

  test('the destructive verb is behind a confirmation that says what it destroys, and nothing is purged by opening it', async ({
    page,
  }) => {
    let purgeRequests = 0
    await page.route('**/api/v1/savings-goals/*/purge', (route) => {
      purgeRequests += 1
      route.fulfill({ status: 204 })
    })

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: /deleted goals \(1\)/i }).click()
    await page.getByRole('button', { name: /delete permanently: kyoto trip/i }).click()

    const dialog = page.getByRole('alertdialog').or(page.getByRole('dialog')).first()
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('Erase this goal for good?')).toBeVisible()
    // The copy must state that the hand-entered money goes too, and must never promise a restore.
    await expect(dialog.getByText(/the ones you entered by hand/)).toBeVisible()
    await expect(dialog.getByText(/can't be undone/)).toBeVisible()

    // Dismiss without confirming: opening the dialog must never destroy anything.
    await page.keyboard.press('Escape')
    await expect(page.getByText('Erase this goal for good?')).toHaveCount(0)
    expect(purgeRequests).toBe(0)

    // And the goal is still listed as kept.
    await expect(page.getByText('Kyoto Trip')).toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Surface 2 — closed-account retained balance history
// ---------------------------------------------------------------------------

test.describe('Retained history — closed account balance history (#1569)', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
    await mockAuthDependencies(page)
    await mockClosedAccountHistory(page)
  })

  test('a CLOSED account still reaches its retained day-by-day balances', async ({ page }) => {
    await page.goto(`/accounts/${CLOSED_ACCOUNT.id}`)
    await page.waitForLoadState('networkidle')

    // By role: the account header's own closed-account sentence also contains the words
    // "balance history", so a bare text lookup is ambiguous.
    await expect(page.getByRole('heading', { name: 'Balance history' })).toBeVisible({
      timeout: 15000,
    })

    // The page as a whole states that the data was KEPT, never that it was destroyed.
    await expect(
      page.getByText(/Closed accounts do not carry a current balance\. Your transaction and balance history is kept\./)
    ).toBeVisible()

    // The surface says what it is, in words rather than by colour, and dates the closure.
    // `exact` because the header sentence above starts with the same two words.
    await expect(page.getByText('Closed account', { exact: true })).toBeVisible()
    await expect(
      page.getByText(/These are the balances we last recorded, not what the account holds today/)
    ).toBeVisible()
    await expect(page.getByText(/it closed on Apr 10, 2026/)).toBeVisible()

    // The retained rows themselves — real dates and real amounts, not a mounted-and-empty table.
    const table = page.locator('table').first()
    await expect(table.locator('tbody tr')).toHaveCount(3)

    const rowFor = (day: string) => table.locator('tbody tr').filter({ hasText: day })
    await expect(rowFor('Apr 8, 2026').getByText('$1,200.50')).toBeVisible()
    await expect(rowFor('Apr 8, 2026').getByText('$1,150.25')).toBeVisible()
    await expect(rowFor('Apr 9, 2026').getByText('$1,310.25')).toBeVisible()
    await expect(rowFor('Apr 9, 2026').getByText('$1,300.75')).toBeVisible()
    await expect(rowFor('Apr 10, 2026').getByText('$980.00')).toBeVisible()

    // A day the provider reported no available figure renders an em dash, never $0.00 — a missing
    // figure must not be presented as an account that held nothing.
    await expect(rowFor('Apr 10, 2026').getByText('\u2014')).toBeVisible()
    await expect(table.getByText('$0.00')).toHaveCount(0)

    // The chart is the accessible-labelled companion to the same numbers, and its label carries
    // no amount (an attribute is recorded verbatim by session replay and cannot be masked).
    const chart = page.getByRole('img', { name: /retained balances over time/i })
    await expect(chart).toBeVisible()
    const chartLabel = await chart.getAttribute('aria-label')
    expect(chartLabel).not.toMatch(/\d/)
  })

  test('a LIVE account shows no retained-history section — the negative control', async ({
    page,
  }) => {
    await page.goto(`/accounts/${LIVE_ACCOUNT.id}`)
    await page.waitForLoadState('networkidle')

    // The page really did render (otherwise the absences below prove nothing).
    await expect(page.getByText('Everyday Checking').first()).toBeVisible({ timeout: 15000 })

    await expect(page.getByRole('heading', { name: 'Balance history' })).toHaveCount(0)
    await expect(page.getByText('Closed account', { exact: true })).toHaveCount(0)
    await expect(page.getByText(/Closed accounts do not carry a current balance/)).toHaveCount(0)
    await expect(
      page.getByText(/These are the balances we last recorded/)
    ).toHaveCount(0)
  })
})
