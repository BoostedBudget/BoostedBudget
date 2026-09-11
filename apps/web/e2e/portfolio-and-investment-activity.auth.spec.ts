/**
 * Portfolio tab + investment activity labelling — Issue #1631.
 *
 * WHY THIS FILE EXISTS. #1631 fixed two USER-VISIBLE symptoms: the Portfolio tab
 * (/insights?tab=portfolio) rendering "Unable to load portfolio data" (a 500 from
 * GetByUserIDWithSecurities failing to scan a non-NULL securities.close_price_as_of — see
 * holding_repository.go and holding_close_price_as_of_integration_test.go), and every brokerage
 * transaction in the feed displaying as a generic "$ Cash" chip with an empty Sub-category. The
 * branch has extensive unit and integration coverage for both, but NOTHING that opens the actual
 * page a user opens — this is that proof.
 *
 * ⚠ NOT MOCKED. Unlike most specs in this directory, this one deliberately does NOT use
 * page.route() to stub the investments/transactions APIs (see cache-freshness.auth.spec.ts and
 * household-infra-smoke.auth.spec.ts for the same real-backend pattern). A route-mocked response
 * could never have caught the actual bug: the 500 originates in the real Postgres driver's (pgx)
 * scan plan for a real DATE column value, which no fixture JSON can reproduce. This spec depends
 * on apps/web/scripts/seed-e2e-data.sql having seeded its investment fixtures (1 holding, 3
 * investment_transactions) against the real E2E database — see that file's "Step 5" for the exact
 * shape and why it is NOT a `subtype = 'dividend reinvestment'` row (a softened fixture that would
 * pass against code with none of #1631's fixes).
 *
 * THE PRODUCTION SHAPE, mirrored by the seed and asserted here: this brokerage reports an ETF
 * distribution as a PAIR of plain cash movements, not as a single `dividend reinvestment`-typed
 * row — `type='cash', subtype='deposit', amount<0, quantity=0` (the dividend leg) and
 * `type='cash', subtype='withdrawal', amount>0, quantity>0` (the reinvest leg). Both the
 * activity-type chip and the Sub-category cell are SERVER-DERIVED values (`activity_type` and a
 * derived subcategory label) — never the raw stored `type`/`subtype` — so the withdrawal leg must
 * read "Reinvest" / "Dividend Reinvestment", not "Cash" / "Withdrawal". A third row shares the
 * exact same raw shape (`cash`/`withdrawal`) but is booked against the seed's CASH-EQUIVALENT
 * security and must still read "Cash" — the negative control proving the fix discriminates on the
 * joined security rather than relabelling every cash/withdrawal row.
 *
 * ROW DISAMBIGUATION. All three investment_transactions rows are against the SAME real security,
 * so TransactionTable's Description cell (ticker · security_name takes priority over the per-row
 * `name`) renders identically for two of the three rows. The seed therefore gives each row a
 * distinct AMOUNT, and this spec locates rows by that amount.
 *
 * ⚠ It located them by NAME until CI ran this spec, and that could never have worked. An
 * investment row's Description column renders `ticker · security_name` — "E2ETF · E2E Test ETF
 * Fund" — and NOT the transaction's own name; the transaction name is only the fallback for when
 * ticker and security name are both blank, which is the M5 rendering fix this very branch added.
 * So all three investment rows share one description and none of them contains the word
 * "Reinvestment". I swapped amounts for names reasoning that names were "more stable" because the
 * seeder sets them, without checking what the UI renders for an investment row — in a feature
 * whose entire point is that investment rows render differently from banking ones.
 *
 * The amounts (24.68 / 18.21 / 31.09) are distinct across the whole fixture and ARE rendered, so
 * they are the discriminator. Every locator keeps `.first()` so a collision with some future
 * fixture row fails as an assertion rather than a strict-mode error.
 * — the only thing in the row guaranteed to be unique.
 */

import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'

test.describe('Portfolio tab renders and investment activity is labelled correctly (#1631)', () => {
  // ⚠ NO `retries: 0` here, deliberately, and it was here until CI ran this spec for the first
  // time. This suite has a pre-existing auth/navigation race: in the same run, five OTHER specs
  // (savings, spending-reports-table, transaction-filtering ×2, url-sort) failed and then PASSED
  // on retry, one of them visibly bouncing to /sign-in. Every one of them recovered because they
  // use the project's retry policy. This spec had opted out, so it was the only genuine failure
  // in the run — not because it is more broken, but because it was the only one unprotected
  // against a flake the whole suite shares.
  //
  // Determinism was the reason to set retries: 0, and it is the wrong trade here: an infra flake
  // failing the build teaches nobody anything, while the assertions below are exact and will fail
  // deterministically if the LABELLING regresses, retries or not.

  test('the Portfolio tab renders real seeded holdings, and the transactions feed derives Reinvest/Dividend/Cash from the real reinvestment pair', async ({
    page,
  }) => {
    await acceptCookieConsent(page)

    // =========================================================================
    // Portfolio tab — the 500 regression guard.
    // =========================================================================
    await page.goto('/insights?tab=portfolio')
    await page.waitForLoadState('domcontentloaded')
    // ⚠ Assert the navigation LANDED before asserting anything about content. When this spec first
    // ran in CI it was bounced to the dashboard, and because the dashboard also carries a
    // portfolio widget with a "Total Value" tile, the tile assertions below PASSED against the
    // wrong page — the failure only surfaced 20 lines later as a missing table row, which says
    // nothing about the real cause. A URL assertion turns that into one obvious line.
    await expect(page).toHaveURL(/\/insights/, { timeout: 15000 })

    // ⚠ ORDER MATTERS HERE. Wait for the tiles FIRST, then assert the error copy is absent.
    // Checked in the other order the absence assertion is VACUOUS: straight after
    // domcontentloaded the summary request has not resolved, so the error copy is legitimately not
    // in the DOM yet and toHaveCount(0) passes against a page that is about to render the failure.
    // The tiles assertion is what actually proves GetPortfolioSummary returned 200 — it times out
    // if the read 500s, which is the regression this spec exists for — and the absence check then
    // rules out a partially-rendered page that shows tiles AND an error.
    await expect(page.getByText('Total Value')).toBeVisible({ timeout: 15000 })
    await expect(page.getByText('Total Gain / Loss')).toBeVisible()
    await expect(page.getByText('Unable to load portfolio data')).toHaveCount(0)

    // The Holdings table shows the seeded security by ticker, with a HUMANISED Type
    // ("ETF", not the raw "etf" — securityTypeLabel in investmentSubcategory.ts).
    const holdingsRow = page.locator('tr').filter({ hasText: 'E2ETF' }).first()
    await expect(holdingsRow).toBeVisible({ timeout: 15000 })
    await expect(holdingsRow.getByText('ETF', { exact: true })).toBeVisible()
    // Negative control: the raw lowercase provider token must never leak through.
    await expect(page.getByText('etf', { exact: true })).toHaveCount(0)

    // =========================================================================
    // Transactions feed — the mislabelling regression guard.
    // =========================================================================
    await page.goto('/transactions')
    await page.waitForLoadState('domcontentloaded')
    await expect(page).toHaveURL(/\/transactions/, { timeout: 15000 })

    // The reinvest leg (cash/withdrawal, amount=24.68, quantity>0): the activity-type chip reads
    // Reinvest and the Sub-category cell reads the DERIVED label "Dividend Reinvestment" — never
    // the raw stored subtype, "Withdrawal".
    const reinvestRow = page.locator('tr').filter({ hasText: '$24.68' }).first()
    await expect(reinvestRow).toBeVisible({ timeout: 15000 })
    await expect(reinvestRow.getByText('Reinvest', { exact: true })).toBeVisible()
    await expect(reinvestRow.getByText('Dividend Reinvestment')).toBeVisible()
    await expect(reinvestRow.getByText('Withdrawal', { exact: true })).toHaveCount(0)

    // The dividend leg (cash/deposit, amount=-18.21, quantity=0): chip reads Dividend, and the
    // Sub-category cell reads the derived "Dividend" label (raw subtype is "Deposit").
    const dividendRow = page.locator('tr').filter({ hasText: '$18.21' }).first()
    await expect(dividendRow).toBeVisible()
    await expect(dividendRow.getByText('Dividend', { exact: true }).first()).toBeVisible()

    // The bill-payment row (cash/withdrawal against the CASH-EQUIVALENT security, amount=31.09):
    // same raw shape as the reinvest leg above, but must still read Cash — proving the fix
    // discriminates on the joined security rather than relabelling every cash/withdrawal row.
    const billPayRow = page.locator('tr').filter({ hasText: '$31.09' }).first()
    await expect(billPayRow).toBeVisible()
    await expect(billPayRow.getByText('Cash', { exact: true })).toBeVisible()
    await expect(billPayRow.getByText('Reinvest', { exact: true })).toHaveCount(0)
  })
})
