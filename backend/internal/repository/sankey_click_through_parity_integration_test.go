//go:build integration

package repository

// Issue #1626 Phase 2, gap G4 — the ONE test that compares the two surfaces the published contract
// talks about.
//
// WHAT IS BEING ASSERTED, AND WHY IT NEEDED ITS OWN FILE. GetTransactionsOutput.TotalAmount's doc
// string states a property to consumers: over the same filtered set,
//
//	|node amount| == |total_amount|
//
// where "node amount" is a category-hierarchy node's rolled-up total. Making that claim CHECKABLE
// was the stated justification for adding whole-set totals to the busiest list in the app, and
// nothing compared the two sides. feed_totals_integration_test.go tests one side against its own
// rows (its header says so, and says this comparison was missing); the hierarchy tests test the
// other side against its own fixture. A property that spans two repositories can only be tested
// where both are called.
//
// ⚠ ABSOLUTE VALUES, NOT EQUALITY, and the difference is the whole reason five earlier comments on
// this branch were wrong. A node's amount is `COALESCE(SUM(ABS(t.amount)), 0)` — an unsigned chart
// quantity — while total_amount is `SUM(t.amount)` under PFCv2 signs, where inflows are NEGATIVE.
// The two numbers coincide for an expense node and differ by a factor of −1 for an income one, so
// the income case below asserts the magnitudes match AND that the signs are opposite. An assertion
// written as plain equality would have to be wrong about one direction or the other.
//
// ⚠ IT MUST FAIL ON A VISIBILITY DRIFT, NOT ONLY ON AN ARITHMETIC ONE. The two surfaces reach the
// same rows through DIFFERENT predicates — that is the actual risk, and it is what
// accountWithNoActiveIntervalLeg exists to close (its own comment says the analytics reads "apply
// no visibility predicate at all, so such a row counts toward the chart and could never appear in
// the list"). So the fixture puts money on an account whose status history has NO 'active' interval
// and on a fully orphaned row (account_id NULL), each admitted to the list by a different disjunct.
// Every seeded amount is distinct and non-round, so a side that loses one row cannot coincide with
// the side that keeps it.
//
// ⚠ WHICH LIST QUERY. Both are asserted. GetByUserIDKeyset (buildWhereClause) carries NO
// account-visibility predicate at all; GetByUserIDKeysetUnified carries
// retainedAccountVisibilityLegs on both arms and is the one the API actually serves, since
// include_investment_activity defaults to true and a category-filtered feed sorts by date. The
// unified path is therefore the one a visibility regression can break, and the banking path is the
// control that says a failure there is about visibility rather than about the filter.
//
// ⚠ THE DIVERGENCES THIS FIXTURE DELIBERATELY DOES NOT CONTAIN, because they are real and
// deliberate rather than defects: the hierarchy read excludes TRANSFER categories
// (buildSpendingFilter's IncludeTransfers leg) and excludes `pending = true` rows, while the
// transactions list applies neither unless asked. The click-through filter below therefore passes
// Pending=false, which is the equivalent question, and the fixture seeds a pending row to prove
// that equivalence is doing work. It seeds no transfers: comparing a node the analytics side cannot
// emit would assert nothing about either query.

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestSankeyClickThrough_NodeAmountAndListTotalDescribeTheSameSet_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	analytics := &analyticsRepository{db: tx}
	transactions := NewTransactionRepository(tx, newTestFieldEncryptor(t))

	userID := uuid.New()
	userAccountID := uuid.New()
	linkedItemID := uuid.New()
	activeAcct := uuid.New()
	terminalOnly := uuid.New()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`,
		userID, "test|sankey-"+userID.String(), "sankey-1626-"+userID.String()+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-sankey-1626','depository')`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "sankey-item-"+linkedItemID.String())
	mustExec("account active", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'Sankey Active','depository','active')`,
		activeAcct, linkedItemID, userID, userAccountID, "plaid-sankey-a-"+activeAcct.String())
	// The retained shape: the insert trigger writes one OPEN 'removed' interval and no 'active'
	// row, so this account's transactions reach the list ONLY through
	// accountWithNoActiveIntervalLeg. The hierarchy read admits them unconditionally.
	mustExec("account terminal-only", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status, removed_at, removed_reason)
		VALUES ($1,$2,$3,$4,$5,'Sankey Terminal Only','depository','removed', now(), 'user_disconnect')`,
		terminalOnly, linkedItemID, userID, userAccountID, "plaid-sankey-t-"+terminalOnly.String())

	// seedTxn writes one settled banking row. accountID nil means a fully orphaned row
	// (account_id NULL, migration 000035's ON DELETE SET NULL residue), which also carries a NULL
	// user_account_id so the list's `OR t.account_id IS NULL` disjunct is the only thing admitting
	// it.
	seedTxn := func(tag string, accountID *uuid.UUID, amount float64, primary, detailed, overrideKind, primaryOverride *string, pending bool) {
		id := uuid.New()
		var acctArg, userAcctArg any
		if accountID != nil {
			acctArg, userAcctArg = *accountID, userAccountID
		}
		mustExec("txn "+tag, `
			INSERT INTO transactions
				(id, user_id, account_id, user_account_id, linked_item_id, source,
				 external_transaction_id, external_transaction_id_blind_idx,
				 amount, date, name, pending,
				 pfc_primary, pfc_detailed, category_override_kind, pfc_primary_override,
				 created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,'plaid',$6,$7,$8,'2026-06-15',$9,$10,$11,$12,$13,$14, now(), now())`,
			id, userID, acctArg, userAcctArg, linkedItemID,
			"ext-sankey-"+id.String(), "blind-sankey-"+id.String(),
			amount, "txn "+tag, pending, primary, detailed, overrideKind, primaryOverride)
	}
	sp := func(s string) *string { return &s }

	const fd = "FOOD_AND_DRINK"

	// ── The FOOD_AND_DRINK expense node: five rows, four ways of getting there ────────────────
	// A second leaf (the detailed row) matters: the node's amount is a ROLL-UP over leaves, so a
	// comparison that read one hierarchy row instead of summing the branch would miss it.
	seedTxn("fd-direct", &activeAcct, 11.11, sp(fd), nil, nil, nil, false)
	seedTxn("fd-groceries", &activeAcct, 22.22, sp(fd), sp("FOOD_AND_DRINK_GROCERIES"), nil, nil, false)
	seedTxn("fd-terminal-only", &terminalOnly, 33.33, sp(fd), nil, nil, nil, false)
	seedTxn("fd-orphan", nil, 44.44, sp(fd), nil, nil, nil, false)
	// An OVERRIDE row: raw category GENERAL_MERCHANDISE, re-filed by the user under FOOD_AND_DRINK.
	// Both surfaces must read the EFFECTIVE category — the list through the generated
	// effective_pfc_primary column, the hierarchy through categoryPathExprs' own CASE — and this row
	// is the only thing that would catch one of them reading the raw column instead.
	seedTxn("fd-override", &activeAcct, 55.55, sp("GENERAL_MERCHANDISE"), nil, sp("pfc"), sp(fd), false)

	// ── Rows that must be in NEITHER side of the FOOD_AND_DRINK expense comparison ────────────
	seedTxn("other-primary", &activeAcct, 66.66, sp("GENERAL_MERCHANDISE"), nil, nil, nil, false)
	seedTxn("fd-inflow", &activeAcct, -77.77, sp(fd), nil, nil, nil, false)
	seedTxn("fd-pending", &activeAcct, 88.88, sp(fd), nil, nil, nil, true)

	// ── The INCOME node, for the sign half of the contract ────────────────────────────────────
	seedTxn("income-wages", &activeAcct, -99.99, sp("INCOME"), sp("INCOME_WAGES"), nil, nil, false)
	seedTxn("income-terminal-only", &terminalOnly, -123.45, sp("INCOME"), nil, nil, nil, false)

	windowStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	settled := false

	// nodeTotals rolls one direction's hierarchy rows up to the named L1 node, the way the Sankey's
	// consolidated view does.
	nodeTotals := func(t *testing.T, direction domain.CategoryDirection, l1Key string) (amount float64, count, leaves int) {
		t.Helper()
		rows, hErr := analytics.GetCategoryHierarchy(ctx, userID.String(), domain.SpendingFilters{
			StartDate: windowStart,
			EndDate:   windowEnd,
		}, direction)
		require.NoError(t, hErr)
		for _, r := range rows {
			if r.L1Key != l1Key {
				continue
			}
			amount += r.Total
			count += r.TransactionCount
			leaves++
		}
		return amount, count, leaves
	}

	cases := []struct {
		name       string
		direction  domain.CategoryDirection
		l1Key      string
		amountType string
		// wantLeaves is asserted so a fixture that collapsed to a single hierarchy row — which would
		// make the roll-up trivial — fails instead of silently weakening the case.
		wantLeaves int
		// wantSignsOppose is true where the contract's two numbers differ by −1: an income node is a
		// positive magnitude while the list's total_amount is negative.
		wantSignsOppose bool
	}{
		{"expense node", domain.CategoryDirectionExpense, fd, "expense", 2, false},
		{"income node", domain.CategoryDirectionIncome, "INCOME", "income", 2, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodeAmount, nodeCount, leaves := nodeTotals(t, tc.direction, tc.l1Key)
			require.NotZero(t, nodeCount, "the node must have rows, or this case asserts nothing")
			require.Equal(t, tc.wantLeaves, leaves,
				"the node must roll up more than one leaf, or the comparison is against a single row")
			require.Positive(t, nodeAmount, "a node amount is an ABS sum and is never negative")

			// The click-through filter: the same window, the same node, and the two equivalences the
			// analytics side applies structurally (direction, settled-only) expressed as list filters.
			listFilters := domain.TransactionFilters{
				StartDate:    &windowStart,
				EndDate:      &windowEnd,
				PFCPrimaries: []string{tc.l1Key},
				AmountType:   &tc.amountType,
				Pending:      &settled,
			}

			assertAgreement := func(t *testing.T, totals *domain.FeedTotals, surface string) {
				t.Helper()
				require.NotNil(t, totals, "%s: the first page must carry whole-set totals", surface)
				assert.Equal(t, nodeCount, totals.Count,
					"%s: the node and the list must describe the same NUMBER of transactions — a "+
						"mismatch is a visibility-universe drift, not an arithmetic one", surface)
				assert.InDelta(t, nodeAmount, math.Abs(totals.Amount), 0.001,
					"%s: |node amount| == |total_amount| is the contract GetTransactionsOutput "+
						"publishes to consumers", surface)
				if tc.wantSignsOppose {
					assert.Negative(t, totals.Amount,
						"%s: an income set's total_amount is negative under PFCv2, which is why the "+
							"contract is on magnitudes and not on equality", surface)
				} else {
					assert.InDelta(t, nodeAmount, totals.Amount, 0.001,
						"%s: for an expense node the two numbers also happen to be equal", surface)
				}
			}

			t.Run("banking keyset list", func(t *testing.T) {
				_, _, totals, lErr := transactions.GetByUserIDKeyset(ctx, userID.String(), listFilters, nil, 2)
				require.NoError(t, lErr)
				assertAgreement(t, totals, "banking keyset")
			})

			t.Run("unified list (the served path)", func(t *testing.T) {
				f := listFilters
				f.IncludeInvestmentActivity = true
				f.SortBy = "date"
				page, _, totals, lErr := transactions.GetByUserIDKeysetUnified(ctx, userID.String(), f, nil, 2)
				require.NoError(t, lErr)
				require.Len(t, page, 2,
					"the page must be shorter than the set, so a page-derived total could not pass")
				assertAgreement(t, totals, "unified")

				// The visibility legs, named rather than merely counted: the terminal-only account's
				// row and the orphaned row must both be IN the list, since the hierarchy counted them.
				all, _, _, aErr := transactions.GetByUserIDKeysetUnified(ctx, userID.String(), f, nil, 100)
				require.NoError(t, aErr)
				var sawTerminalOnly, sawOrphan bool
				for _, item := range all {
					switch item.AccountID {
					case terminalOnly.String():
						sawTerminalOnly = true
					case "":
						// ActivityItem.AccountID is a plain string, so a NULL account_id arrives as
						// the empty string rather than as a nil pointer.
						sawOrphan = true
					}
				}
				assert.True(t, sawTerminalOnly,
					"a row on an account with no 'active' interval counts toward the node, so the "+
						"list must show it (accountWithNoActiveIntervalLeg)")
				if tc.direction == domain.CategoryDirectionExpense {
					assert.True(t, sawOrphan,
						"an orphaned row (account_id NULL) counts toward the node, so the list must "+
							"show it (the `OR t.account_id IS NULL` disjunct)")
				}
			})
		})
	}

	t.Run("the node is a strict subset of the unfiltered feed", func(t *testing.T) {
		// Without this, a filter that silently matched everything would satisfy every assertion
		// above: the node total would equal the list total because both would be the whole ledger.
		_, _, totals, lErr := transactions.GetByUserIDKeyset(ctx, userID.String(), domain.TransactionFilters{
			StartDate: &windowStart,
			EndDate:   &windowEnd,
		}, nil, 2)
		require.NoError(t, lErr)
		require.NotNil(t, totals)

		nodeAmount, nodeCount, _ := nodeTotals(t, domain.CategoryDirectionExpense, fd)
		assert.Greater(t, totals.Count, nodeCount,
			"the fixture must hold rows outside the node — a different primary, an inflow and a "+
				"pending row — or the comparison proves nothing about the filter")
		assert.Greater(t, math.Abs(math.Abs(totals.Amount)-nodeAmount), 0.001,
			"and their amounts must differ, so an equality that ignored the filter would fail")
	})
}
