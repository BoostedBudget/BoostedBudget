// analytics_tie_order_determinism_integration_test.go — #1569 gap task G4, issue #1585.
//
// The six spending reads (GetSpendingByCategory / GetSpendingByMerchant / GetSpendingTrends, each
// with a household sibling) sorted only by `total` (and `period_start`). Rows tied on the sort key
// therefore came back in whatever order the executor emitted them, so the SAME data could answer
// the SAME question in two different orders once the plan changed. That is how #1585 was found: a
// before/after equivalence sweep over #1569 T3 saw identical result MULTISETS and a different row
// ORDER inside equal sort keys.
//
// The ruling (issue #1585) is ALPHABETICAL BY NAME, applied identically to the personal and
// household member of every pair.
//
// Each read is run under FOUR planner configurations and the order must be identical across all of
// them AND equal to the documented alphabetical order. A single-plan test cannot see this defect
// and passed against the pre-fix code.
//
// ⚠ The MERCHANT reads' third sort leg — the grouped merchant key — is pinned STRUCTURALLY by
// TestSpendingReads_OrderByCarriesTheDocumentedTiebreaker in
// analytics_tie_order_sql_shape_test.go, not here. Removing that leg leaves the order of two
// same-named merchant groups UNDEFINED, and an undefined order that happens to match on this
// machine's hash layout is exactly what a semantic test cannot pin. This file still builds the
// collision end-to-end (four distinct merchant keys rendering the identical name "Unknown"), which
// is what proves the leg is reachable rather than theoretical.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TieOrder -v
package repository_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// tiePlans is the planner matrix. enable_hashagg is the lever that actually moves tie order:
// a HashAggregate emits groups in hash-bucket order, a GroupAggregate emits them in GROUP BY key
// order, so the two hand the final Sort its input in different orders and, with no tiebreaker,
// produce different answers. enable_seqscan is the knob #1585 names; it is kept so the matrix
// covers the scan shape too.
var tiePlans = []struct {
	name string
	gucs []string
}{
	{"hashagg=on,seqscan=on", []string{"SET LOCAL enable_hashagg = on", "SET LOCAL enable_seqscan = on"}},
	{"hashagg=off,seqscan=on", []string{"SET LOCAL enable_hashagg = off", "SET LOCAL enable_seqscan = on"}},
	{"hashagg=on,seqscan=off", []string{"SET LOCAL enable_hashagg = on", "SET LOCAL enable_seqscan = off"}},
	{"hashagg=off,seqscan=off", []string{"SET LOCAL enable_hashagg = off", "SET LOCAL enable_seqscan = off"}},
}

// underEachPlan runs read() once per planner configuration on a dedicated transaction (so
// `SET LOCAL` cannot leak to another test through the shared pool) and returns the row order each
// produced. The transaction is always rolled back; the reads are read-only.
func underEachPlan(t *testing.T, pool *pgxpool.Pool, read func(repository.AnalyticsRepository) []string) map[string][]string {
	t.Helper()
	ctx := context.Background()
	out := make(map[string][]string, len(tiePlans))
	for _, plan := range tiePlans {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err, "begin tx for plan %s", plan.name)
		func() {
			defer func() { _ = tx.Rollback(ctx) }()
			for _, guc := range plan.gucs {
				_, err := tx.Exec(ctx, guc)
				require.NoError(t, err, "%s: %s", plan.name, guc)
			}
			out[plan.name] = read(repository.NewAnalyticsRepository(tx))
		}()
	}
	return out
}

// assertPlanIndependentOrder pins both halves of #1585's acceptance: every plan agrees, and what
// they agree on is the documented alphabetical order.
func assertPlanIndependentOrder(t *testing.T, got map[string][]string, expected []string) {
	t.Helper()
	for _, plan := range tiePlans {
		assert.Equal(t, expected, got[plan.name],
			"plan %s returned a different order for rows tied on the sort key; equal-total rows must be ordered alphabetically by name, not by whatever the executor emitted", plan.name)
	}
}

// tieOrderScope is one user with one account shared into their own household, so the SAME seeded
// rows can be read through both the personal and the household query of a pair.
type tieOrderScope struct {
	UserID    string
	AccountID string
	LinkedID  string
	Filters   domain.SpendingFilters
	HHFilters domain.SpendingFilters
	Date      string
	Month     string
}

func newTieOrderScope(t *testing.T, pool *pgxpool.Pool) tieOrderScope {
	t.Helper()
	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	acct := setupHouseholdMemberAccount(t, pool, owner)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, acct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	// Every transaction lands on ONE day, so the trends reads collapse to a single period and the
	// period_start leg of their ORDER BY cannot mask a tie-order difference.
	day := now.AddDate(0, 0, -5)

	base := domain.SpendingFilters{StartDate: now.AddDate(0, -1, 0), EndDate: now}
	hhFilters := base
	hhFilters.HouseholdScope = &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}

	return tieOrderScope{
		UserID:    owner,
		AccountID: acct.AccountID,
		LinkedID:  acct.LinkedItemID,
		Filters:   base,
		HHFilters: hhFilters,
		Date:      day.Format("2006-01-02"),
		Month:     day.Format("2006-01"),
	}
}

// insertTieTransaction seeds one expense. effective_category_name is a plain column the Go layer
// populates at write time; a raw insert must set it or the non-pointer string scan fails on NULL.
func (s tieOrderScope) insertTieTransaction(t *testing.T, pool *pgxpool.Pool, pfc, display string, amount float64, merchantID *string, merchantName string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO transactions (user_id, account_id, linked_item_id, source, name,
			merchant_name, merchant_id, amount, date, pending, pfc_primary, effective_category_name)
		VALUES ($1, $2, $3, 'plaid', 'Tie Txn', $4, $5, $6, $7, false, $8, $9)
	`, s.UserID, s.AccountID, s.LinkedID, merchantName, merchantID, amount, s.Date, pfc, display)
	require.NoError(t, err, "seed tie transaction %s/%s", pfc, merchantName)
}

// insertTieMerchant creates a merchants row (a global table with no user_id, hence the explicit
// cleanup) and, when displayAs is non-empty, a per-user display_name override for it.
func insertTieMerchant(t *testing.T, pool *pgxpool.Pool, userID, name, displayAs string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO merchants (name, normalized_name, created_at) VALUES ($1, $2, NOW())
		RETURNING id::text
	`, name, name).Scan(&id), "seed merchant %s", name)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM merchants WHERE id = $1`, id) //nolint:errcheck
	})
	if displayAs != "" {
		_, err := pool.Exec(ctx, `
			INSERT INTO merchant_name_overrides (user_id, merchant_id, display_name, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
		`, userID, id, displayAs)
		require.NoError(t, err, "seed display_name override %s -> %s", name, displayAs)
	}
	return id
}

// ─── category / trends fixture ───────────────────────────────────────────────────────────────
//
// Four categories, each totalling exactly $100, so every row ties on `total` and the tiebreaker
// alone decides the order. The PFC keys are chosen so GROUP BY key order is the exact REVERSE of
// display-name order — which is what a GroupAggregate emits, and therefore what the pre-fix code
// returned under `enable_hashagg = off`:
//
//	BANK_FEES -> "Zulu Tie"        ENTERTAINMENT       -> "Yankee Tie"
//	FOOD_AND_DRINK -> "Xray Tie"   GENERAL_MERCHANDISE -> "Whiskey Tie"
//
// The display names are deliberately unrelated to the PFC keys. That is not artificial: in
// production `effective_category_name` for a custom-override row is a name the USER chose, and its
// collation order has nothing to do with the `custom:<uuid>` grouping key. PFC keys are used here
// only because they give a DETERMINISTIC key order to contrast the name order against — a
// custom-category fixture would tie on unpredictable uuids and could not assert an exact order.
func setupCategoryTieFixture(t *testing.T, pool *pgxpool.Pool) tieOrderScope {
	t.Helper()
	s := newTieOrderScope(t, pool)
	s.insertTieTransaction(t, pool, "BANK_FEES", "Zulu Tie", 100, nil, "Tie Merchant")
	s.insertTieTransaction(t, pool, "ENTERTAINMENT", "Yankee Tie", 100, nil, "Tie Merchant")
	s.insertTieTransaction(t, pool, "FOOD_AND_DRINK", "Xray Tie", 50, nil, "Tie Merchant")
	s.insertTieTransaction(t, pool, "FOOD_AND_DRINK", "Xray Tie", 50, nil, "Tie Merchant")
	s.insertTieTransaction(t, pool, "GENERAL_MERCHANDISE", "Whiskey Tie", 100, nil, "Tie Merchant")
	return s
}

// expectedCategoryOrder is alphabetical by display name — the exact reverse of the GROUP BY key
// order the pre-fix query returned under a GroupAggregate.
var expectedCategoryOrder = []string{
	"Whiskey Tie|100.00", "Xray Tie|100.00", "Yankee Tie|100.00", "Zulu Tie|100.00",
}

// ─── merchant fixture ────────────────────────────────────────────────────────────────────────
//
// Six merchant groups, each totalling exactly $100, FOUR of which render the identical display
// name "Unknown" — the collision the merchant reads' COALESCE chain makes reachable and that
// alphabetical-by-name alone therefore cannot resolve:
//
//	raw:Alpha        -> "Alpha"        (merchant_name 'Alpha', no merchants row)
//	merchant:<u1>    -> "Unknown"      (a merchants row with a display_name override of 'Unknown')
//	merchant:<u2>    -> "Unknown"      (ditto — two distinct keys, one name)
//	raw:Unknown      -> "Unknown"      (merchant_name literally 'Unknown', 2 txns)
//	unknown          -> "Unknown"      (no merchant_id, empty merchant_name)
//	merchant:<zulu>  -> "Zulu <uuid>"  (an ordinary merchants row)
//
// The four "Unknown" rows are told apart in the result by merchant_id (the two merchant-backed
// ones) and by transaction_count (2 vs 1 for the other two) — without that the assertion could not
// distinguish one order from another.
type merchantTieFixture struct {
	tieOrderScope
	Expected []string
}

func setupMerchantTieFixture(t *testing.T, pool *pgxpool.Pool) merchantTieFixture {
	t.Helper()
	s := newTieOrderScope(t, pool)
	short := s.UserID[:8]

	u1 := insertTieMerchant(t, pool, s.UserID, "UnkSrcA "+short, "Unknown")
	u2 := insertTieMerchant(t, pool, s.UserID, "UnkSrcB "+short, "Unknown")
	zuluName := "Zulu " + short
	zulu := insertTieMerchant(t, pool, s.UserID, zuluName, "")

	const cat = "GENERAL_MERCHANDISE"
	s.insertTieTransaction(t, pool, cat, "Merchant Tie", 100, nil, "Alpha")  // raw:Alpha
	s.insertTieTransaction(t, pool, cat, "Merchant Tie", 100, &u1, "")       // merchant:<u1>
	s.insertTieTransaction(t, pool, cat, "Merchant Tie", 100, &u2, "")       // merchant:<u2>
	s.insertTieTransaction(t, pool, cat, "Merchant Tie", 50, nil, "Unknown") // raw:Unknown
	s.insertTieTransaction(t, pool, cat, "Merchant Tie", 50, nil, "Unknown") //
	s.insertTieTransaction(t, pool, cat, "Merchant Tie", 100, nil, "")       // unknown
	s.insertTieTransaction(t, pool, cat, "Merchant Tie", 100, &zulu, "")     // merchant:<zulu>

	// The two merchant-backed "Unknown" groups are separated only by their grouped key,
	// 'merchant:<uuid>', so the expected order is derived by sorting those keys the way the
	// query does rather than hard-coded against a uuid this run happened to generate.
	unknownIDs := []string{u1, u2}
	sort.Strings(unknownIDs)

	expected := []string{"Alpha|100.00|1|-"}
	for _, id := range unknownIDs {
		expected = append(expected, fmt.Sprintf("Unknown|100.00|1|%s", id))
	}
	expected = append(expected,
		"Unknown|100.00|2|-",                          // raw:Unknown  ('r' < 'u')
		"Unknown|100.00|1|-",                          // unknown
		fmt.Sprintf("%s|100.00|1|%s", zuluName, zulu), // 'Z' sorts last by name
	)
	return merchantTieFixture{tieOrderScope: s, Expected: expected}
}

func renderCategories(rows []domain.SpendingByCategory) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s|%.2f", r.PFCPrimaryDisplay, r.Total))
	}
	return out
}

func renderMerchants(rows []domain.SpendingByMerchant) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		id := "-"
		if r.MerchantID != nil {
			id = *r.MerchantID
		}
		out = append(out, fmt.Sprintf("%s|%.2f|%d|%s", r.MerchantName, r.Total, r.TransactionCount, id))
	}
	return out
}

func renderTrends(rows []domain.SpendingTrendRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s|%s|%.2f", r.PeriodStart.Format("2006-01"), r.CategoryDisplay, r.Total))
	}
	return out
}

func TestAnalyticsRepository_TieOrder_SpendingByCategory_IsPlanIndependent(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	s := setupCategoryTieFixture(t, pool)
	ctx := context.Background()

	t.Run("personal", func(t *testing.T) {
		got := underEachPlan(t, pool, func(r repository.AnalyticsRepository) []string {
			rows, err := r.GetSpendingByCategory(ctx, s.UserID, s.Filters)
			require.NoError(t, err)
			require.Len(t, rows, len(expectedCategoryOrder))
			return renderCategories(rows)
		})
		assertPlanIndependentOrder(t, got, expectedCategoryOrder)
	})

	t.Run("household", func(t *testing.T) {
		got := underEachPlan(t, pool, func(r repository.AnalyticsRepository) []string {
			rows, err := r.GetSpendingByCategory(ctx, s.UserID, s.HHFilters)
			require.NoError(t, err)
			require.Len(t, rows, len(expectedCategoryOrder))
			return renderCategories(rows)
		})
		assertPlanIndependentOrder(t, got, expectedCategoryOrder)
	})
}

func TestAnalyticsRepository_TieOrder_SpendingTrends_IsPlanIndependent(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	s := setupCategoryTieFixture(t, pool)
	ctx := context.Background()

	expected := make([]string, 0, len(expectedCategoryOrder))
	for _, c := range expectedCategoryOrder {
		expected = append(expected, s.Month+"|"+c)
	}

	t.Run("personal", func(t *testing.T) {
		got := underEachPlan(t, pool, func(r repository.AnalyticsRepository) []string {
			rows, err := r.GetSpendingTrends(ctx, s.UserID, s.Filters)
			require.NoError(t, err)
			require.Len(t, rows, len(expected))
			return renderTrends(rows)
		})
		assertPlanIndependentOrder(t, got, expected)
	})

	t.Run("household", func(t *testing.T) {
		got := underEachPlan(t, pool, func(r repository.AnalyticsRepository) []string {
			rows, err := r.GetSpendingTrends(ctx, s.UserID, s.HHFilters)
			require.NoError(t, err)
			require.Len(t, rows, len(expected))
			return renderTrends(rows)
		})
		assertPlanIndependentOrder(t, got, expected)
	})
}

func TestAnalyticsRepository_TieOrder_SpendingByMerchant_IsPlanIndependent(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	f := setupMerchantTieFixture(t, pool)
	ctx := context.Background()

	t.Run("personal", func(t *testing.T) {
		got := underEachPlan(t, pool, func(r repository.AnalyticsRepository) []string {
			rows, err := r.GetSpendingByMerchant(ctx, f.UserID, f.Filters, 20)
			require.NoError(t, err)
			require.Len(t, rows, len(f.Expected))
			return renderMerchants(rows)
		})
		assertPlanIndependentOrder(t, got, f.Expected)
	})

	t.Run("household", func(t *testing.T) {
		got := underEachPlan(t, pool, func(r repository.AnalyticsRepository) []string {
			rows, err := r.GetSpendingByMerchant(ctx, f.UserID, f.HHFilters, 20)
			require.NoError(t, err)
			require.Len(t, rows, len(f.Expected))
			return renderMerchants(rows)
		})
		assertPlanIndependentOrder(t, got, f.Expected)
	})
}
