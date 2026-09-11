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
 * distinct NAME, and this spec locates rows by that name rather than by their amount. Amounts were
 * the first choice and are the wrong key: the seeder also inserts ordinary banking transactions, so
 * an amount substring can collide with a row this spec knows nothing about, and a colliding
 * `filter()` without `.first()` is a Playwright strict-mode failure rather than a clear assertion.
 * The names are set by the seeder two files away and are unique to these fixtures.
 * — the only thing in the row guaranteed to be unique.
 */

import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'

test.describe('Portfolio tab renders and investment activity is labelled correctly (#1631)', () => {
  test.describe.configure({ retries: 0 })

  test('the Portfolio tab renders real seeded holdings, and the transactions feed derives Reinvest/Dividend/Cash from the real reinvestment pair', async ({
    page,
  }) => {
    await acceptCookieConsent(page)

    // =========================================================================
    // Portfolio tab — the 500 regression guard.
    // =========================================================================
    await page.goto('/insights?tab=portfolio')
    await page.waitForLoadState('domcontentloaded')

    // Assert the ERROR STATE IS ABSENT explicitly. A page that fails to load could otherwise
    // satisfy a loose "tiles exist eventually" check by simply never resolving — the failure mode
    // this branch actually had (a 500 on GetPortfolioSummary).
    await expect(page.getByText('Unable to load portfolio data')).toHaveCount(0)

    // The summary tiles render with real seeded numbers (PortfolioSummaryCard).
    await expect(page.getByText('Total Value')).toBeVisible({ timeout: 15000 })
    await expect(page.getByText('Total Gain / Loss')).toBeVisible()

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

    // The reinvest leg (cash/withdrawal, amount=24.68, quantity>0): the activity-type chip reads
    // Reinvest and the Sub-category cell reads the DERIVED label "Dividend Reinvestment" — never
    // the raw stored subtype, "Withdrawal".
    const reinvestRow = page.locator('tr').filter({ hasText: 'E2E Test ETF Fund Reinvestment' }).first()
    await expect(reinvestRow).toBeVisible({ timeout: 15000 })
    await expect(reinvestRow.getByText('Reinvest', { exact: true })).toBeVisible()
    await expect(reinvestRow.getByText('Dividend Reinvestment')).toBeVisible()
    await expect(reinvestRow.getByText('Withdrawal', { exact: true })).toHaveCount(0)

    // The dividend leg (cash/deposit, amount=-18.21, quantity=0): chip reads Dividend, and the
    // Sub-category cell reads the derived "Dividend" label (raw subtype is "Deposit").
    const dividendRow = page.locator('tr').filter({ hasText: 'E2E Test ETF Fund Dividend' }).first()
    await expect(dividendRow).toBeVisible()
    await expect(dividendRow.getByText('Dividend', { exact: true }).first()).toBeVisible()

    // The bill-payment row (cash/withdrawal against the CASH-EQUIVALENT security, amount=31.09):
    // same raw shape as the reinvest leg above, but must still read Cash — proving the fix
    // discriminates on the joined security rather than relabelling every cash/withdrawal row.
    const billPayRow = page.locator('tr').filter({ hasText: 'E2E Advisory Fee Payment' }).first()
    await expect(billPayRow).toBeVisible()
    await expect(billPayRow.getByText('Cash', { exact: true })).toBeVisible()
    await expect(billPayRow.getByText('Reinvest', { exact: true })).toHaveCount(0)
  })
})
