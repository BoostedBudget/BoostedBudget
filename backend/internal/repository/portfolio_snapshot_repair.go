package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ── #1569 Task 11 (S6/E4): repairing recorded portfolio history, coverage-limited ─────────────
//
// WHY THE OBVIOUS IMPLEMENTATION IS THE BUG
// Upsert recomputes a day's aggregate as SUM(h.institution_value) over CURRENT holdings, and there
// is no price-history table anywhere in this system. Both of Upsert's callers pass today, so that
// is harmless in normal operation — but a repair is BY DEFINITION a past-day recompute. Rebuilding
// a past day the same way would silently overwrite recorded history with today's prices: a
// portfolio curve that is wrong in a completely plausible way, on days when nothing happened.
// That is why holding_snapshots exists (task 10), and it is the only source this repair reads for
// values. `holdings` is read here for exactly one purpose — deciding whether an account is the
// kind of account that bears holdings at all — and never for a number.
//
// WHAT "COVERAGE" MEANS, AND WHY IT IS NOT `present`
// Two different questions are asked of holding_snapshots, and answering them with the same filter
// is the mistake this design exists to avoid:
//
//   - "Did the capture OBSERVE this account on this day?" → does any row exist for
//     (account, day), present or not. A row's existence is the observation; `present` is what the
//     observation said.
//   - "What did the account HOLD on this day?" → the present rows only.
//
// The pair is what makes a confirmed liquidation representable. A day whose rows all read
// present = false is positive evidence of an empty day, and is repaired to zero. A day with NO
// rows is not evidence of anything and is left alone (below).
//
// THE THREE STATES (task 10 §S3), and the ruling on each
//
//	(a) NO rows for the day        → REFUSE. Never write a zero. Binding (ruling R36). A day can
//	                                 lack rows because it predates the history tables, or because a
//	                                 partial sync skipped the account and the daily sweep never ran.
//	                                 "No rows" is indistinguishable from "held nothing".
//	(b) PARTIAL coverage           → REFUSE. This is my ruling, and the brief left it open.
//	                                 Rebuilding from an incomplete row set yields a number that is
//	                                 wrong in a PLAUSIBLE way — a dip on a day nothing happened —
//	                                 which is strictly worse than an obvious gap, because nothing
//	                                 downstream can tell it is wrong. A refusal is legible; a
//	                                 plausible lie is not. Recorded as refused_partial_coverage
//	                                 with both counts, so the decision is re-derivable.
//	(c) OVER-reporting rows        → closed at the source by task 10 (`present` + the liveness
//	                                 predicate). The repair inherits both: it filters on present and
//	                                 applies the SAME accountLiveOnDateClause Upsert applies.
//
// SCOPE, deliberately narrow
//   - Only days STRICTLY BEFORE the server's UTC today. Today's history is still being written and
//     the daily job rewrites today's aggregate anyway; pinning a verdict on today would freeze a
//     mid-day partial as settled history.
//   - Only days that ALREADY have a portfolio_snapshots row. The repair fixes what was recorded; it
//     never invents an aggregate for a day the product never wrote.
//   - Only days not already in portfolio_snapshot_repairs. That is what makes a second run a no-op.

// repairSelectSQL decides and applies every unexamined past day for one user in a single
// statement, and returns the verdict counts.
//
// It is one statement on purpose. The analysis, the UPDATE and the ledger INSERT see the same
// snapshot, so a verdict cannot be recorded for a state other than the one it was computed from,
// and a crash cannot leave a repaired row without its ledger entry (or the reverse).
//
// The ledger CTE reads `updated`'s RETURNING to tell 'repaired' from 'verified': the UPDATE is
// guarded by IS DISTINCT FROM, so a day whose stored aggregate already matched produces no row
// there — and is recorded as verified without so much as touching updated_at.
//
// It is a template rather than a finished string for one mechanical reason worth knowing before
// "simplifying" it: cache_completeness_write_set_test.go derives every mutating method's write set
// by finding package-level SQL constants that are ONE raw string literal. A value assembled with
// `+` and a function call is invisible to that extractor, which fails closed — and an unresolved
// write set is indistinguishable from an empty one, which would certify this method as
// cache-irrelevant without proving it. The liveness predicate is substituted at call time instead.
const repairSelectSQL = `
WITH candidate AS (
    SELECT ps.user_id, ps.snapshot_date,
           ps.total_value, ps.total_cost_basis, ps.total_gain_loss, ps.total_gain_loss_pct
    FROM portfolio_snapshots ps
    WHERE ps.user_id = $1::uuid
      AND ps.snapshot_date < (now() AT TIME ZONE 'UTC')::date
      AND NOT EXISTS (
          SELECT 1 FROM portfolio_snapshot_repairs r
          WHERE r.user_id = ps.user_id AND r.snapshot_date = ps.snapshot_date)
),
-- The accounts recorded as live on each candidate day. Same predicate Upsert applies, rendered
-- from the same string (account_liveness_sql.go) — a rebuild that disagreed with Upsert about
-- which accounts belong to a day would reintroduce task 10's Critical 1 from the other side.
live AS (
    SELECT c.snapshot_date, a.id AS account_id
    FROM candidate c
    JOIN accounts a ON a.user_id = c.user_id
    WHERE $LIVENESS
),
-- The accounts a complete day WOULD have a record for: live, and known to bear holdings. The
-- evidence is either a history row on ANY day or a current holdings row. Current holdings decide
-- membership of this set ONLY — no value is ever read from them.
expected AS (
    SELECT l.snapshot_date, l.account_id
    FROM live l
    WHERE EXISTS (SELECT 1 FROM holding_snapshots hb WHERE hb.account_id = l.account_id)
       OR EXISTS (SELECT 1 FROM holdings hc WHERE hc.account_id = l.account_id)
),
-- Observed = the capture ran for this account on this day. Deliberately NOT filtered on present:
-- a day whose rows are all absent is an observed EMPTY day, not an unobserved one.
observed AS (
    SELECT l.snapshot_date, l.account_id
    FROM live l
    WHERE EXISTS (
        SELECT 1 FROM holding_snapshots hs
        WHERE hs.account_id = l.account_id
          AND hs.as_of_date = l.snapshot_date
          AND hs.user_id = $1::uuid)
),
-- The reconstruction. Mirrors Upsert's four columns EXACTLY, including the asymmetry that makes
-- the last two non-derivable from the first two: gain/loss is summed over only the rows where
-- cost_basis AND institution_value are both non-NULL (NULL basis would be a phantom gain, NULL
-- value a phantom loss), and the pct denominator is that FILTERED basis, not total_cost_basis.
rebuilt AS (
    SELECT l.snapshot_date,
           COALESCE(SUM(hs.institution_value), 0) AS total_value,
           COALESCE(SUM(hs.cost_basis), 0) AS total_cost_basis,
           COALESCE(SUM(hs.institution_value) FILTER (WHERE hs.cost_basis IS NOT NULL AND hs.institution_value IS NOT NULL), 0)
             - COALESCE(SUM(hs.cost_basis) FILTER (WHERE hs.cost_basis IS NOT NULL AND hs.institution_value IS NOT NULL), 0)
             AS total_gain_loss,
           CASE
               WHEN COALESCE(SUM(hs.cost_basis) FILTER (WHERE hs.cost_basis IS NOT NULL AND hs.institution_value IS NOT NULL), 0) > 0
               THEN round((
                       (COALESCE(SUM(hs.institution_value) FILTER (WHERE hs.cost_basis IS NOT NULL AND hs.institution_value IS NOT NULL), 0)
                        - SUM(hs.cost_basis) FILTER (WHERE hs.cost_basis IS NOT NULL AND hs.institution_value IS NOT NULL))
                       / SUM(hs.cost_basis) FILTER (WHERE hs.cost_basis IS NOT NULL AND hs.institution_value IS NOT NULL)
                    ) * 100, 4)
               ELSE NULL
           END AS total_gain_loss_pct
    FROM live l
    JOIN holding_snapshots hs
      ON hs.account_id = l.account_id
     AND hs.as_of_date = l.snapshot_date
     AND hs.user_id = $1::uuid
     AND hs.present
    GROUP BY l.snapshot_date
),
decided AS (
    SELECT c.user_id, c.snapshot_date,
           c.total_value AS prev_total_value,
           c.total_cost_basis AS prev_total_cost_basis,
           c.total_gain_loss AS prev_total_gain_loss,
           c.total_gain_loss_pct AS prev_total_gain_loss_pct,
           (SELECT count(*) FROM expected e WHERE e.snapshot_date = c.snapshot_date)::int AS expected_count,
           (SELECT count(*) FROM observed o WHERE o.snapshot_date = c.snapshot_date)::int AS observed_count,
           COALESCE(rb.total_value, 0) AS new_total_value,
           COALESCE(rb.total_cost_basis, 0) AS new_total_cost_basis,
           COALESCE(rb.total_gain_loss, 0) AS new_total_gain_loss,
           rb.total_gain_loss_pct AS new_total_gain_loss_pct,
           CASE
               WHEN NOT EXISTS (SELECT 1 FROM observed o WHERE o.snapshot_date = c.snapshot_date)
                   THEN 'refused_no_coverage'
               WHEN EXISTS (
                   SELECT 1 FROM expected e
                   WHERE e.snapshot_date = c.snapshot_date
                     AND NOT EXISTS (
                         SELECT 1 FROM observed o
                         WHERE o.snapshot_date = e.snapshot_date AND o.account_id = e.account_id))
                   THEN 'refused_partial_coverage'
               ELSE 'repairable'
           END AS verdict
    FROM candidate c
    LEFT JOIN rebuilt rb ON rb.snapshot_date = c.snapshot_date
),
updated AS (
    UPDATE portfolio_snapshots ps
    SET total_value         = d.new_total_value,
        total_cost_basis    = d.new_total_cost_basis,
        total_gain_loss     = d.new_total_gain_loss,
        total_gain_loss_pct = d.new_total_gain_loss_pct
    FROM decided d
    WHERE ps.user_id = d.user_id
      AND ps.snapshot_date = d.snapshot_date
      AND d.verdict = 'repairable'
      -- Write only a day that actually disagrees. An already-correct day must come out of this
      -- repair byte-identical, updated_at included, or "the repair changed nothing the second
      -- time" stops being observable.
      AND (ps.total_value, ps.total_cost_basis, ps.total_gain_loss, ps.total_gain_loss_pct)
          IS DISTINCT FROM
          (d.new_total_value, d.new_total_cost_basis, d.new_total_gain_loss, d.new_total_gain_loss_pct)
    RETURNING ps.snapshot_date
),
ledger AS (
    INSERT INTO portfolio_snapshot_repairs (
        user_id, snapshot_date, outcome, reason,
        expected_account_count, observed_account_count,
        previous_total_value, previous_total_cost_basis,
        previous_total_gain_loss, previous_total_gain_loss_pct,
        repaired_total_value, repaired_total_cost_basis,
        repaired_total_gain_loss, repaired_total_gain_loss_pct)
    SELECT d.user_id, d.snapshot_date,
        CASE
            WHEN d.verdict <> 'repairable' THEN d.verdict
            WHEN u.snapshot_date IS NOT NULL THEN 'repaired'
            ELSE 'verified'
        END,
        CASE
            WHEN d.verdict = 'refused_no_coverage' THEN
                'no holding_snapshots rows for this day: unreconstructable, left untouched rather '
                || 'than zeroed (the day may predate the history tables, or a partial sync may have '
                || 'skipped every account with no daily sweep behind it)'
            WHEN d.verdict = 'refused_partial_coverage' THEN
                format('partial coverage: %s of %s live holdings-bearing accounts have rows for this '
                    || 'day, so a rebuild would under-report by an amount nothing downstream could '
                    || 'detect', d.observed_count, d.expected_count)
            WHEN u.snapshot_date IS NOT NULL THEN
                format('rebuilt from holding_snapshots across %s covered account(s)', d.observed_count)
            ELSE
                format('stored aggregate already matched the holding_snapshots reconstruction across '
                    || '%s covered account(s); nothing written', d.observed_count)
        END,
        d.expected_count, d.observed_count,
        d.prev_total_value, d.prev_total_cost_basis, d.prev_total_gain_loss, d.prev_total_gain_loss_pct,
        -- The accepted reconstruction. NULL on a refusal, where none was accepted — repeating the
        -- previous value there would read like a write that never happened.
        CASE WHEN d.verdict = 'repairable' THEN d.new_total_value END,
        CASE WHEN d.verdict = 'repairable' THEN d.new_total_cost_basis END,
        CASE WHEN d.verdict = 'repairable' THEN d.new_total_gain_loss END,
        CASE WHEN d.verdict = 'repairable' THEN d.new_total_gain_loss_pct END
    FROM decided d
    LEFT JOIN updated u ON u.snapshot_date = d.snapshot_date
    ON CONFLICT (user_id, snapshot_date) DO NOTHING
    RETURNING outcome
)
SELECT outcome, count(*)::int FROM ledger GROUP BY outcome
`

// RepairUserFromHistory repairs one user's unexamined past portfolio_snapshots days. See the
// commentary above repairSelectSQL for what it will and will not touch.
func (r *portfolioSnapshotRepository) RepairUserFromHistory(ctx context.Context, userID string) (domain.PortfolioRepairSummary, error) {
	var summary domain.PortfolioRepairSummary

	// $LIVENESS is substituted from the one shared predicate (account_liveness_sql.go) — the same
	// string Upsert and the holdings capture render. Both operands are compile-time constants; no
	// user input reaches this substitution.
	query := strings.ReplaceAll(repairSelectSQL, "$LIVENESS",
		accountLiveOnDateSQL("a.id", "c.snapshot_date"))

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return summary, fmt.Errorf("portfolio repair for user %s: %w", userID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var outcome string
		var n int
		if err := rows.Scan(&outcome, &n); err != nil {
			return domain.PortfolioRepairSummary{}, fmt.Errorf("portfolio repair for user %s: scan verdict: %w", userID, err)
		}
		summary.Add(outcome, n)
	}
	if rows.Err() != nil {
		return domain.PortfolioRepairSummary{}, fmt.Errorf("portfolio repair for user %s: %w", userID, rows.Err())
	}
	return summary, nil
}

// UserIDsWithUnexaminedSnapshots enumerates users the repair still owes a verdict.
//
// It enumerates from portfolio_snapshots, NOT from holdings. GetDistinctUserIDsWithHoldings — the
// enumeration the daily job uses for its own work — joins a.status = 'active' and therefore skips
// a user who has disconnected every institution. That user's recorded curve is precisely the one
// most likely to be wrong, because the aggregate that was written while a wrong liveness predicate
// was live is still on disk and no sync will ever revisit it.
const userIDsWithUnexaminedSnapshotsSQL = `
	SELECT DISTINCT ps.user_id::text
	FROM portfolio_snapshots ps
	WHERE ps.snapshot_date < (now() AT TIME ZONE 'UTC')::date
	  AND NOT EXISTS (
	      SELECT 1 FROM portfolio_snapshot_repairs r
	      WHERE r.user_id = ps.user_id AND r.snapshot_date = ps.snapshot_date)
	ORDER BY 1
	LIMIT $1::int
`

func (r *portfolioSnapshotRepository) UserIDsWithUnexaminedSnapshots(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	rows, err := r.db.Query(ctx, userIDsWithUnexaminedSnapshotsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("enumerate users with unexamined portfolio snapshots: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("enumerate users with unexamined portfolio snapshots: scan: %w", err)
		}
		out = append(out, id)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("enumerate users with unexamined portfolio snapshots: %w", rows.Err())
	}
	return out, nil
}
