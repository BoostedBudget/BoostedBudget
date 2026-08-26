//go:build t3bench

// Equality harness for the #1569 T3 (#1582) "option (b) anti-join" candidate.
//
// WHY THIS FILE EXISTS. The round-5 table in docs/decisions/2026-08-26-household-grants-cte-performance.md
// (it lived in householdGrantsCTE's doc comment until #1569 round 6) records an anti-join rewrite at
// 76.2ms vs HEAD's 136.2ms and asserts, in prose, that its "output is identical to HEAD on all
// four columns". That assertion carried NO diff method, NO row count and NO reproducer, while
// the row two lines above it (the no-tiebreak variant) carried both its numbers and its
// falsification. A perf rewrite of the predicate all nine household reads depend on may not ship
// on an unreproducible equality claim, so this harness re-establishes (or refutes) equality
// mechanically: it runs BOTH forms of all nine reads against the seeded 200k-transaction
// household and compares FULL result sets -- row count and every column of every row.
//
// It does not compare against a golden file. It compares the two forms against each other in the
// same process, against the same database, in the same transaction-free session, so a difference
// can only come from the predicate.
//
// Run exactly like TestT3Bench (same tag, same URL, same seeded throwaway database):
//
//	T3BENCH_URL=postgres://... go test -tags t3bench -run TestT3BenchAntiJoinEquality \
//	    ./internal/repository/ -v
//
// ⚠ A PASS HERE IS NOT A PROOF OF EQUIVALENCE. It is a proof of agreement ON THIS SEED. The seed
// has 50 user_accounts each holding exactly one open grant plus, for 30 of them, closed grants
// that do not overlap their open one. It therefore never exercises the shape the two forms are
// known to disagree on -- see TestT3BenchAntiJoinIsNotEquivalent below, which builds that shape
// explicitly and is the reason the candidate is NOT shipped.
package repository

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// candidateAntiJoinPredicate is the "option (b), losers ANTI JOIN" form, written WITHOUT a day
// expansion. The recorded variant expanded grant intervals into days with generate_series, which
// is what made it need an upper date bound getMonthOverMonthSpendingHousehold does not have. That
// expansion is unnecessary: a top-level correlated NOT EXISTS is pulled up by the planner into a
// Hash Anti Join on the hashable `g.user_account_id = a.user_account_id` leg, with the date
// comparisons applied as a join filter. Same plan shape, no day expansion, no date bound.
//
// The rewrite splits HEAD's single EXISTS-with-a-nested-NOT-EXISTS into two independent top-level
// subqueries -- and that split is exactly where its semantics change. See the counterexample test.
func candidateAntiJoinPredicate(txAlias, accountAlias string) string {
	covers := func(grantAlias string) string {
		return fmt.Sprintf(
			`(%[1]s.date + 1) > %[2]s.share_started_at
			   AND (%[2]s.share_ended_at IS NULL OR %[1]s.date < %[2]s.share_ended_at)`,
			txAlias, grantAlias)
	}
	return fmt.Sprintf(`(EXISTS (
				SELECT 1 FROM household_grants g
				WHERE g.user_account_id = %[1]s.user_account_id
				  AND %[2]s
			) AND NOT EXISTS (
				SELECT 1 FROM household_grants g
				JOIN household_grants g2
				  ON g2.fingerprint = g.fingerprint
				 AND g2.user_account_id <> g.user_account_id
				 AND (g2.share_started_at, g2.user_account_id) < (g.share_started_at, g.user_account_id)
				 AND %[3]s
				WHERE g.user_account_id = %[1]s.user_account_id
				  AND g.fingerprint_contested
				  AND %[2]s
			))`, accountAlias, covers("g"), covers("g2"))
}

func t3benchPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("T3BENCH_URL")
	if url == "" {
		t.Fatal("T3BENCH_URL not set — NOTHING MEASURED. -tags t3bench asks for a benchmark; " +
			"a skip here would be indistinguishable from a pass. See t3bench_capture_test.go.")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// runAll executes sql and returns every row rendered as one string per row, sorted. Sorting is
// deliberate: four of the nine reads are single-row aggregates and the rest carry an ORDER BY, but
// an ORDER BY with ties is not a total order, so comparing sorted multisets tests SET equality
// without importing tie-order flakiness. #1585's tie-order tests are what pin the ORDER BY.
func runAll(ctx context.Context, t *testing.T, pool *pgxpool.Pool, label, sql string, args []interface{}) []string {
	t.Helper()
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("%s: query: %v", label, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("%s: values: %v", label, err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = fmt.Sprintf("%v", v)
		}
		out = append(out, strings.Join(parts, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: iterate: %v", label, err)
	}
	sort.Strings(out)
	return out
}

func TestT3BenchAntiJoinEquality(t *testing.T) {
	ctx := context.Background()
	pool := t3benchPool(t)

	start := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	cf := domain.CashFlowFilters{StartDate: start, EndDate: end, HouseholdScope: benchScope(), Grouping: "monthly"}
	sf := domain.SpendingFilters{StartDate: start, EndDate: end, HouseholdScope: benchScope()}

	cases := []struct {
		name string
		run  func(r AnalyticsRepository) error
	}{
		{"CashFlowAggregation", func(r AnalyticsRepository) error {
			_, e := r.GetCashFlowAggregation(ctx, benchUser1, cf)
			return e
		}},
		{"CashFlowGrouped", func(r AnalyticsRepository) error {
			_, e := r.GetCashFlowGrouped(ctx, benchUser1, cf)
			return e
		}},
		{"SpendingByCategory", func(r AnalyticsRepository) error {
			_, e := r.GetSpendingByCategory(ctx, benchUser1, sf)
			return e
		}},
		{"SpendingByMerchant", func(r AnalyticsRepository) error {
			_, e := r.GetSpendingByMerchant(ctx, benchUser1, sf, 20)
			return e
		}},
		{"SpendingTrends", func(r AnalyticsRepository) error {
			_, e := r.GetSpendingTrends(ctx, benchUser1, sf)
			return e
		}},
		{"AverageMonthlySpend", func(r AnalyticsRepository) error {
			_, e := r.GetAverageMonthlySpend(ctx, benchUser1, sf)
			return e
		}},
		{"SavingsRateForPeriod", func(r AnalyticsRepository) error {
			_, e := r.GetSavingsRateForPeriod(ctx, benchUser1, start, end, benchScope())
			return e
		}},
		{"MonthOverMonthSpending", func(r AnalyticsRepository) error {
			_, e := r.GetMonthOverMonthSpending(ctx, benchUser1, start, "", sf)
			return e
		}},
		{"IncomeSourceBreakdown", func(r AnalyticsRepository) error {
			_, e := r.GetIncomeSourceBreakdown(ctx, benchUser1, sf)
			return e
		}},
	}

	headPredicate := householdSharedTransactionPredicate("t", "a")
	candPredicate := candidateAntiJoinPredicate("t", "a")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := &capturingDB{inner: pool}
			if err := tc.run(NewAnalyticsRepository(cap)); err != nil {
				t.Fatalf("capture: %v", err)
			}
			headSQL := cap.sql
			if headSQL == "" {
				t.Fatal("no SQL captured")
			}
			if !strings.Contains(headSQL, headPredicate) {
				t.Fatalf("captured SQL does not contain the HEAD predicate verbatim; the harness "+
					"can no longer derive the candidate by substitution. SQL:\n%s", headSQL)
			}
			candSQL := strings.ReplaceAll(headSQL, headPredicate, candPredicate)
			if candSQL == headSQL {
				t.Fatal("substitution produced identical SQL")
			}

			headRows := runAll(ctx, t, pool, "head", headSQL, cap.args)
			candRows := runAll(ctx, t, pool, "candidate", candSQL, cap.args)

			if len(headRows) != len(candRows) {
				t.Fatalf("row count differs: head=%d candidate=%d", len(headRows), len(candRows))
			}
			diffs := 0
			for i := range headRows {
				if headRows[i] != candRows[i] {
					diffs++
					if diffs <= 5 {
						t.Errorf("row %d differs:\n  head      = %s\n  candidate = %s", i, headRows[i], candRows[i])
					}
				}
			}
			if diffs > 0 {
				t.Fatalf("%d of %d rows differ", diffs, len(headRows))
			}
			t.Logf("identical: %d rows, all columns", len(headRows))

			// Timing, best of 5, same instrument and run count as TestT3Bench. Quoted as
			// "best of 5" wherever it is cited -- see t3bench_capture_test.go's header.
			timeIt := func(sql string) time.Duration {
				var best time.Duration
				for i := 0; i < 5; i++ {
					t0 := time.Now()
					runAll(ctx, t, pool, "timing", sql, cap.args)
					if d := time.Since(t0); best == 0 || d < best {
						best = d
					}
				}
				return best
			}
			h, c := timeIt(headSQL), timeIt(candSQL)
			t.Logf("timing best-of-5: head=%v candidate=%v (%.2fx)", h, c, float64(h)/float64(c))

			// T3BENCH_EXPLAIN=1 prints both plans. A timing without the plan that produced it is
			// how this ticket accumulated three wrong recorded numbers, so the instrument that
			// measures also shows its work.
			if os.Getenv("T3BENCH_EXPLAIN") != "" {
				for _, p := range []struct{ label, sql string }{{"head", headSQL}, {"candidate", candSQL}} {
					rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, SETTINGS) "+p.sql, cap.args...)
					if err != nil {
						t.Fatalf("%s explain: %v", p.label, err)
					}
					var lines []string
					for rows.Next() {
						var l string
						if err := rows.Scan(&l); err != nil {
							t.Fatal(err)
						}
						lines = append(lines, l)
					}
					rows.Close()
					t.Logf("%s plan:\n%s", p.label, strings.Join(lines, "\n"))
				}
			}
		})
	}
}

// ── The refutation ───────────────────────────────────────────────────────────────────────────────
//
// ⚠ THIS TEST ASSERTS A DEFECT. It passes while the candidate is WRONG. It is a decision record
// with an executable body, not a rule to preserve: if a corrected candidate ever makes it fail,
// that is the signal to re-derive the rejection below, not to weaken the assertions.
//
// TestT3BenchAntiJoinEquality passing is agreement on ONE dataset, so it cannot license the
// rewrite on its own. This test asks the logical question the seed cannot: is splitting HEAD's
// single `EXISTS(g : winner(g))` into `EXISTS(g : covers(g)) AND NOT EXISTS(g : loser(g))` an
// identity?
//
// It is not. The first says SOME covering grant wins; the second says EVERY covering grant wins.
// They differ exactly when one user_account holds TWO grants covering the SAME day, one winning
// and one losing -- and that shape is ordinary, not pathological. uq_household_account_shares_active
// forbids two UN-REVOKED grants per (household, user_account) and nothing else, so unsharing an
// account and re-sharing it later the same day produces two grants; the day-overlap predicate
// (deliberately generous at both ends -- see householdSharedTransactionPredicate) then has both of
// them covering that boundary day.
//
// It builds the grant set inline as a VALUES relation named `household_grants` and evaluates the
// two predicates -- the REAL ones, obtained from the production functions, not transcriptions --
// as scalars. It needs a Postgres connection but NOT the seed: it reads no table.
func TestT3BenchAntiJoinIsNotEquivalent(t *testing.T) {
	ctx := context.Background()
	pool := t3benchPool(t)

	const (
		u1 = "00000000-0000-0000-0000-0000000000f1"
		u2 = "00000000-0000-0000-0000-0000000000f2"
	)

	// probe evaluates both predicates for one (grant set, day, user_account) triple.
	probe := func(t *testing.T, grants []string, day, ua string) (head, cand bool) {
		t.Helper()
		q := fmt.Sprintf(`
			WITH household_grants AS (
				SELECT user_account_id, fingerprint, share_started_at, share_ended_at,
				       MIN(user_account_id::text) OVER (PARTITION BY fingerprint)
				         <> MAX(user_account_id::text) OVER (PARTITION BY fingerprint)
				         AS fingerprint_contested
				FROM (VALUES %s) AS v(user_account_id, fingerprint, share_started_at, share_ended_at)
			),
			t(date) AS (VALUES (DATE '%s')),
			a(user_account_id) AS (VALUES ('%s'::uuid))
			SELECT %s, %s FROM t CROSS JOIN a`,
			strings.Join(grants, ", "), day, ua,
			householdSharedTransactionPredicate("t", "a"),
			candidateAntiJoinPredicate("t", "a"))
		if err := pool.QueryRow(ctx, q).Scan(&head, &cand); err != nil {
			t.Fatalf("probe: %v\n%s", err, q)
		}
		return head, cand
	}

	grant := func(ua, fp, from, to string) string {
		end := "NULL::timestamp"
		if to != "" {
			end = "TIMESTAMP '" + to + "'"
		}
		return fmt.Sprintf("('%s'::uuid, '%s'::text, TIMESTAMP '%s', %s)", ua, fp, from, end)
	}

	t.Run("control_plain_joint_account_agrees", func(t *testing.T) {
		// The shape TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountCountedOnce
		// pins: two members, one fingerprint, one grant each. Exactly one wins under BOTH forms.
		// Without this control a divergence below could just mean the probe returns false always.
		g := []string{
			grant(u1, "F1", "2023-01-01 00:00", ""),
			grant(u2, "F1", "2023-03-01 00:00", ""),
		}
		h1, c1 := probe(t, g, "2023-05-10", u1)
		h2, c2 := probe(t, g, "2023-05-10", u2)
		if !h1 || !c1 || h2 || c2 {
			t.Fatalf("control diverged or degenerate: u1 head=%v cand=%v, u2 head=%v cand=%v", h1, c1, h2, c2)
		}
	})

	t.Run("same_day_reshare_under_a_new_fingerprint", func(t *testing.T) {
		// u1 shares account A (fingerprint F1, joint with u2 who shared earlier and therefore
		// outranks u1). At 18:00 on 2023-05-10 u1 unshares; at 20:00 the same day the account is
		// re-linked and re-shared, now fingerprinted F2 (uncontested, u1 alone holds it).
		// On 2023-05-10 u1 holds a LOSING F1 grant and a WINNING F2 grant.
		g := []string{
			grant(u1, "F1", "2023-05-10 00:00", "2023-05-10 18:00"),
			grant(u2, "F1", "2023-01-01 00:00", ""),
			grant(u1, "F2", "2023-05-10 20:00", ""),
		}
		head, cand := probe(t, g, "2023-05-10", u1)
		if !head {
			t.Fatalf("HEAD must keep u1's own uncontested F2 account on this day, got %v", head)
		}
		if cand {
			t.Fatal("expected the candidate to DROP it; it now agrees with HEAD. If the candidate " +
				"was corrected, re-derive the whole rejection — this test asserts a defect, not a rule")
		}
	})

	t.Run("same_day_reshare_loses_the_joint_account_entirely", func(t *testing.T) {
		// Worse, and same fingerprint throughout. u1 shares in January; u2 (the other holder of
		// the same physical account) shares in March; u1 unshares at 18:00 on 2023-05-10 and
		// re-shares at 20:00. HEAD counts u1's copy on that day and only u1's -- the dedup holds.
		// The candidate drops u1 (its 20:00 grant loses to u2) AND u2 (loses to u1's January
		// grant), so the joint account's whole day disappears from every household aggregate.
		g := []string{
			grant(u1, "F1", "2023-01-01 00:00", "2023-05-10 18:00"),
			grant(u2, "F1", "2023-03-01 00:00", ""),
			grant(u1, "F1", "2023-05-10 20:00", ""),
		}
		h1, c1 := probe(t, g, "2023-05-10", u1)
		h2, c2 := probe(t, g, "2023-05-10", u2)
		if h1 == c1 && h2 == c2 {
			t.Fatalf("expected divergence, got agreement: u1 head=%v cand=%v, u2 head=%v cand=%v", h1, c1, h2, c2)
		}
		if c1 || c2 {
			t.Fatalf("expected the candidate to drop BOTH copies, got u1=%v u2=%v", c1, c2)
		}
		if !h1 || h2 {
			t.Fatalf("expected HEAD to keep exactly u1's copy, got u1=%v u2=%v", h1, h2)
		}
	})
}
