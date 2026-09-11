package repository

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1569 T6 — append-only holdings history, written BESIDE unchanged current state.
//
// holdingRepository.ReplaceForAccount DELETEs an account's holdings and re-INSERTs them on every
// sync, so `holdings` can only ever answer "what is held right now". This file adds the "what was
// held then" half. Three properties are deliberate and load-bearing:
//
//  1. holdings keeps its replace semantics EXACTLY as they were. Soft-deleting holdings instead
//     was ruled out twice: every SUM(h.institution_value) would need a filter (one miss
//     double-counts a portfolio), and portfolio_snapshot_repository.go's Upsert re-derives a PAST
//     day's total from the live holdings rows — moving them would send those recomputes to zero.
//
//  2. The sync capture runs inside ReplaceForAccount's OWN transaction, after its INSERTs. So the
//     history row is derived from the committed current state, and a rolled-back replace leaves
//     no history behind either. There is no window in which the two disagree.
//
//  3. A capture is a SET, not a list of additions. Fix round 1: an INSERT ... SELECT alone is a
//     positive-only record — it can say "this position existed" and has no way to say "and
//     nothing else". A confirmed-complete liquidation therefore selected zero rows, wrote
//     nothing, and left an EARLIER capture the same day standing as that day's answer (measured:
//     live holdings 0, day's history 5000.00). So every capture now runs two statements: mark
//     every row it did not re-observe for that (account, UTC day) present = false, then upsert
//     the ones it did as present = true. Nothing is deleted — an absent row keeps its last known
//     values — and reconstruction filters on present.

// serverUTCDate reads the UTC calendar date from the SERVER, not the Go process.
//
// Every capture takes its as_of_date from here and passes it as a parameter, for two reasons.
// (a) It cannot drift with the session TimeZone the way a bare date cast would — production is
// only UTC because NewPool pins it (the #1461 lesson), and a psql session or a migration inherits
// no such pin. (b) portfolio_snapshot_worker.go pairs a capture with a portfolio_snapshots upsert
// and MUST file both under the same day; taking the aggregate's day from the Go clock and the
// rows' day from the server meant that across UTC midnight — with the ~16 ms host/container skew
// this project already documents — the aggregate and the rows behind it landed on DIFFERENT days,
// which is the precise failure the pairing exists to prevent.
func serverUTCDate(ctx context.Context, db DB) (time.Time, error) {
	var d time.Time
	if err := db.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'UTC')::date`).Scan(&d); err != nil {
		return time.Time{}, err
	}
	return d, nil
}

// markHoldingSnapshotsAbsentForAccountSQL is half one of a capture: everything recorded for this
// account on this UTC day that the account no longer holds stopped being held. Covers both a
// security sold between two same-day captures and a confirmed-complete liquidation (in which case
// it marks the whole day's row set absent and the INSERT below adds nothing).
//
// $1 = account_id, $2 = user_id, $3 = as_of_date.
const markHoldingSnapshotsAbsentForAccountSQL = `
	UPDATE holding_snapshots hs
	SET present = FALSE, captured_at = now()
	WHERE hs.account_id = $1
	  AND hs.user_id = $2
	  AND hs.as_of_date = $3::date
	  AND hs.present
	  AND NOT EXISTS (
	      SELECT 1 FROM holdings h
	      WHERE h.account_id = hs.account_id
	        AND h.security_id = hs.security_id
	        AND h.user_id = hs.user_id
	  )
`

// captureHoldingSnapshotsSQL is half two: record what the account DOES hold.
//
// Grain: (account_id, security_id, as_of_date). A second capture on the same day UPDATEs that
// day's row rather than appending, so the table grows with elapsed days, not with sync frequency.
// present = TRUE is re-asserted on conflict so a security that goes away and comes back the same
// day reads correctly.
//
// $1 = account_id, $2 = user_id, $3 = as_of_date. accountID and userID both scope it, matching the
// enclosing DELETE/INSERT pair, so the capture cannot reach a foreign row.
const captureHoldingSnapshotsSQL = `
	INSERT INTO holding_snapshots (
		user_id, account_id, user_account_id, security_id,
		as_of_date, captured_at, present,
		quantity, cost_basis, institution_value, institution_price, currency_code
	)
	SELECT
		h.user_id, h.account_id, h.user_account_id, h.security_id,
		$3::date, now(), TRUE,
		-- #1631 M8a: the currency is copied FROM THE HOLDING at capture time, so a historical
		-- position stays denominated in the currency it was actually reported in rather than in
		-- whatever the live row says months later. Same reasoning as every other value column
		-- here: the history must not drift with current state.
		h.quantity, h.cost_basis, h.institution_value, h.institution_price, h.currency_code
	FROM holdings h
	WHERE h.account_id = $1 AND h.user_id = $2
	ON CONFLICT (account_id, security_id, as_of_date) DO UPDATE SET
		user_id           = EXCLUDED.user_id,
		user_account_id   = EXCLUDED.user_account_id,
		captured_at       = EXCLUDED.captured_at,
		present           = TRUE,
		quantity          = EXCLUDED.quantity,
		cost_basis        = EXCLUDED.cost_basis,
		institution_value = EXCLUDED.institution_value,
		institution_price = EXCLUDED.institution_price,
		currency_code     = EXCLUDED.currency_code
`

// recordHoldingObservationSQL is half three of a capture, and the only half that runs when there
// is nothing to record: it writes down that the capture COMPLETED for this (account, UTC day).
//
// The other two halves are both position-shaped, so an account observed holding NOTHING leaves no
// trace at all — the mark-absent can only touch rows that already exist, and the INSERT ... SELECT
// selects nothing. For an account with prior history the day's all-absent rows carry the
// observation; for one WITHOUT prior history (a re-link successor whose first sync comes back
// empty) nothing does, and the portfolio re-link rule then cannot tell "confirmed empty" from
// "not synced yet" and reports the predecessor's stale valuation. See migration 000176.
//
// $1 = user_id, $2 = account_id, $3 = as_of_date.
const recordHoldingObservationSQL = `
	INSERT INTO holding_observations (user_id, account_id, as_of_date, observed_at)
	VALUES ($1, $2, $3::date, now())
	ON CONFLICT (account_id, as_of_date) DO UPDATE SET
		user_id     = EXCLUDED.user_id,
		observed_at = EXCLUDED.observed_at
`

// captureHoldingSnapshots records one account's post-replace position SET. db is expected to be
// the pgx.Tx that just performed the replace, so both halves and the replace commit together.
//
// Deliberately carries NO liveness filter, unlike the whole-user form below. Checked in review
// round 2 and found unreachable rather than merely unlikely: the only caller is
// holdingRepository.ReplaceForAccount, which a sync reaches only for accounts
// GetByLinkedItemID returned, and that method filters status = 'active' — and the
// account_status_history trigger keeps an active account's interval OPEN (valid_to IS NULL),
// which satisfies the overlap test for any day. Adding the filter here would be dead code that
// implies a reachable case. Recorded so the next reader does not re-derive it; if
// GetByLinkedItemID is ever widened, this becomes reachable and the filter must be added.
// It RETURNS the day it filed under — #1569 T9.1 item C. The whole-user form already did, and for
// the same reason: whoever pairs an aggregate with this capture must file both under one day, and
// the only way to be sure of that is to take the day from the capture rather than to compute a
// second one from a second clock.
func captureHoldingSnapshots(ctx context.Context, db DB, accountID, userID string) (time.Time, error) {
	day, err := serverUTCDate(ctx, db)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := db.Exec(ctx, markHoldingSnapshotsAbsentForAccountSQL, accountID, userID, day); err != nil {
		return time.Time{}, err
	}
	if _, err := db.Exec(ctx, captureHoldingSnapshotsSQL, accountID, userID, day); err != nil {
		return time.Time{}, err
	}
	if _, err := db.Exec(ctx, recordHoldingObservationSQL, userID, accountID, day); err != nil {
		return time.Time{}, err
	}
	return day, nil
}

// markHoldingSnapshotsAbsentForUserSQL / captureHoldingSnapshotsForUserSQL are the whole-user form,
// and exist for one reason: portfolio_snapshot_worker.go upserts a portfolio_snapshots aggregate
// for EVERY user with holdings once a day, whether or not anything synced. A capture that only
// fired on a sync would leave days that have an aggregate and no rows behind it — and task 11
// rebuilds those aggregates FROM these rows.
//
// $1 = user_id, $2 = as_of_date.
//
// ⚠ AND THERE IS NO OBSERVATION HALF HERE, deliberately — the third statement the per-account
// capture runs (recordHoldingObservationSQL) is absent from this form on purpose, and putting it
// back is a money bug. holding_observations means "we ASKED THE PROVIDER and this is what it
// said". The per-account capture can say that: investment_sync_service reaches ReplaceForAccount
// only for an account PRESENT in a confirmed-complete /investments/holdings/get response, so zero
// holdings there is a real confirmed-empty answer. This sweep asks nobody — it re-reads our own
// `holdings` table for every user daily, whether or not anything synced. A marker from here would
// certify "confirmed empty" for a re-link successor created minutes ago that has not synced yet,
// the exclusion rule would drop its retained predecessor, and the user's portfolio would fall to
// ZERO for the day: the exact cliff leg 3 of that rule exists to prevent, re-created by the fix
// for a different hole. Found on self-review of that fix and pinned by
// TestPortfolioSnapshot_DailySweepDoesNotVouchForAnAccountItNeverAsked.
//
// ⚠ THAT TEST NAME IS BROADER THAN WHAT IT PINNED, and #1569 T10.3 item B is the measurement that
// closed the gap rather than a fix. There are TWO routes by which an account can look observed —
// the repair's `observed` CTE (portfolio_snapshot_repair.go) accepts a marker OR holding_snapshots
// for the day — and this sweep writes no markers but plainly DOES write holding_snapshots. So the
// named test spoke for one arm and the name spoke for both.
//
// Measured, not argued: the holdings route cannot vouch for an unasked account either, and
// structurally so. The INSERT below is `SELECT ... FROM holdings`, so it can only write a row for
// an account that already bears holdings, and `holdings` has exactly ONE writer outside tests —
// holdingRepository.ReplaceForAccount, which a sync reaches only for an account present in a
// confirmed-complete provider response. The never-synced re-link successor this whole comment is
// about has nothing for the sweep to copy, so the sweep writes it nothing to be observed by.
// TestPortfolioSnapshot_DailySweepCannotVouchViaTheHoldingsRouteEither is that measurement, with a
// control proving the zero is an absent vouch rather than an absent sweep. What the sweep's rows
// ARE is a restatement of an ASKED account's last known positions on a later day — the sweep's
// stated purpose above, and symmetric with what the repair sums for that day. No behaviour changed.
//
// The mark-absent half deliberately omits the liveness clause its paired INSERT carries: it only
// ever touches rows that ALREADY exist for this day, and a row exists for a terminal account only
// if a capture put it there while the account was still live that same day — in which case the
// account overlaps the day and the INSERT re-observes it anyway, so nothing is marked. Checked in
// review round 2; no failing case could be constructed. Adding the clause would be a no-op that
// implies one exists.
const markHoldingSnapshotsAbsentForUserSQL = `
	UPDATE holding_snapshots hs
	SET present = FALSE, captured_at = now()
	WHERE hs.user_id = $1::uuid
	  AND hs.as_of_date = $2::date
	  AND hs.present
	  AND NOT EXISTS (
	      SELECT 1 FROM holdings h
	      WHERE h.account_id = hs.account_id
	        AND h.security_id = hs.security_id
	        AND h.user_id = hs.user_id
	  )
`

var captureHoldingSnapshotsForUserSQL = `
	INSERT INTO holding_snapshots (
		user_id, account_id, user_account_id, security_id,
		as_of_date, captured_at, present,
		quantity, cost_basis, institution_value, institution_price, currency_code
	)
	SELECT
		h.user_id, h.account_id, h.user_account_id, h.security_id,
		$2::date, now(), TRUE,
		-- #1631 M8a: the currency is copied FROM THE HOLDING at capture time, so a historical
		-- position stays denominated in the currency it was actually reported in rather than in
		-- whatever the live row says months later. Same reasoning as every other value column
		-- here: the history must not drift with current state.
		h.quantity, h.cost_basis, h.institution_value, h.institution_price, h.currency_code
	FROM holdings h
	WHERE h.user_id = $1::uuid
	  AND ` + accountLiveOnDateSQL("h.account_id", "$2") + `
	ON CONFLICT (account_id, security_id, as_of_date) DO UPDATE SET
		user_id           = EXCLUDED.user_id,
		user_account_id   = EXCLUDED.user_account_id,
		captured_at       = EXCLUDED.captured_at,
		present           = TRUE,
		quantity          = EXCLUDED.quantity,
		cost_basis        = EXCLUDED.cost_basis,
		institution_value = EXCLUDED.institution_value,
		institution_price = EXCLUDED.institution_price,
		currency_code     = EXCLUDED.currency_code
`

// The liveness predicate above is rendered from accountLiveOnDateSQL (account_liveness_sql.go) —
// the SAME string portfolio_snapshot_repository.go's Upsert and the portfolio repair render. It
// carries placeholders rather than hard-coded parameter numbers so callers that number their
// parameters differently can still share one piece of text. Substituted at package init on a
// compile-time constant, so there is no user input path.

// HoldingSnapshotRepository reads the history the captures write, and exposes the whole-user
// capture the daily job needs.
type HoldingSnapshotRepository interface {
	// CaptureForUser records the user's positions for today's UTC day, restricted to accounts
	// that were live at some point during that day — the same restriction Upsert applies. Returns
	// the SERVER's UTC date it filed the rows under, so the caller can file its aggregate under
	// exactly the same day rather than under the Go process's idea of today.
	//
	// Idempotent within the day: re-running refreshes that day's rows rather than appending.
	CaptureForUser(ctx context.Context, userID string) (time.Time, error)

	// GetForUserOnDate returns the user's PRESENT positions on the given UTC calendar day — the
	// reconstruction primitive task 11 needs. SUMming institution_value / cost_basis over the
	// result reproduces the aggregate portfolio_snapshot_repository.go's Upsert computes from live
	// holdings, for a day whose live holdings have since been replaced.
	//
	// ⚠ Pass a UTC-located time.Time. pgx binds a time.Time's own calendar day, so a value located
	// in, say, America/New_York at 21:00 binds as that local date even though the instant is
	// already the next day in UTC.
	GetForUserOnDate(ctx context.Context, userID string, asOf time.Time) ([]domain.HoldingSnapshot, error)

	// GetByAccountDateRange returns every retained row for one account across a UTC date range,
	// oldest first, INCLUDING rows marked absent — it is the history browser, not the
	// reconstruction primitive, and an absent row is a fact worth showing. Independent of the
	// account's CURRENT status, so a disconnected brokerage's history stays queryable.
	GetByAccountDateRange(ctx context.Context, accountID string, start, end time.Time) ([]domain.HoldingSnapshot, error)

	// ListHistoryDatesForAccount returns every UTC day on which this account has at least one
	// RETAINED row, newest first — present or absent. Tenancy-scoped in SQL by userID, exactly
	// like the live holdings read — GetByAccountDateRange above takes no user_id and predates any
	// caller, and nothing user-facing may be built on an unscoped read.
	//
	// ⚠ RETAINED ROWS, NOT PRESENT ONES, AND THAT IS THE POINT (#1569 T7.8). A day whose every
	// row is absent is a confirmed-complete liquidation: we looked, and the account held nothing.
	// That is a TRUE OBSERVATION we kept, and it is a different fact from "we kept nothing for
	// that day". Filtering this listing on `present` collapsed the two and made the surface tell
	// the user their retained day did not exist — the defect #1569 is named for, inside the
	// surface built to fix it. The day is offered; GetHistoryForAccountOnDate below then answers
	// it with zero positions, and the caller says "held nothing on D" rather than "nothing kept".
	//
	// Deliberately independent of the account's CURRENT status: a closed brokerage is the whole
	// reason this exists (privacy policy 8.2a), so a liveness filter here would re-create the very
	// hole it closes.
	ListHistoryDatesForAccount(ctx context.Context, userID, accountID string) ([]string, error)

	// GetHistoryForAccountOnDate returns the account's PRESENT positions on one UTC day, joined
	// with security metadata. Present-only: an absent row records that the position STOPPED being
	// held, and rendering it as a holding would state a sold position as an owned one.
	//
	// ⚠ ASYMMETRIC WITH ListHistoryDatesForAccount ABOVE, ON PURPOSE. That one counts retained
	// rows so a fully-liquidated day is still offered; this one counts present rows so that day
	// renders EMPTY. Empty here therefore means "the account held nothing on this day", never
	// "this day was not kept" — the caller must not turn it back into a not-kept claim.
	GetHistoryForAccountOnDate(ctx context.Context, userID, accountID, asOf string) ([]domain.HoldingSnapshotView, error)
}

type holdingSnapshotRepository struct {
	db DB
}

func NewHoldingSnapshotRepository(db DB) HoldingSnapshotRepository {
	return &holdingSnapshotRepository{db: db}
}

func (r *holdingSnapshotRepository) CaptureForUser(ctx context.Context, userID string) (time.Time, error) {
	// Both halves in one transaction: a mark-absent that committed without its matching insert
	// would read as a liquidation that did not happen.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	day, err := serverUTCDate(ctx, tx)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, markHoldingSnapshotsAbsentForUserSQL, userID, day); err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, captureHoldingSnapshotsForUserSQL, userID, day); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return day, nil
}

const holdingSnapshotColumns = `
	hs.id, hs.user_id, hs.account_id, hs.user_account_id, hs.security_id,
	hs.as_of_date, hs.captured_at, hs.present,
	hs.quantity, hs.cost_basis, hs.institution_value, hs.institution_price,
	hs.currency_code, hs.created_at, hs.updated_at`

func (r *holdingSnapshotRepository) GetForUserOnDate(ctx context.Context, userID string, asOf time.Time) ([]domain.HoldingSnapshot, error) {
	// No MAX(captured_at) selection any more, and that is a simplification the presence marker
	// bought: a capture marks what it did not observe absent, so the present rows for a
	// (account, day) ARE that day's last capture by construction. There is no dedup rule for a
	// reader to get wrong.
	query := `
		SELECT` + holdingSnapshotColumns + `
		FROM holding_snapshots hs
		WHERE hs.user_id = $1::uuid
		  AND hs.as_of_date = $2::date
		  AND hs.present
		ORDER BY hs.account_id, hs.security_id
	`
	return r.queryHoldingSnapshots(ctx, query, userID, asOf)
}

func (r *holdingSnapshotRepository) GetByAccountDateRange(ctx context.Context, accountID string, start, end time.Time) ([]domain.HoldingSnapshot, error) {
	query := `
		SELECT` + holdingSnapshotColumns + `
		FROM holding_snapshots hs
		WHERE hs.account_id = $1::uuid
		  AND hs.as_of_date >= $2::date
		  AND hs.as_of_date <= $3::date
		ORDER BY hs.as_of_date, hs.security_id
	`
	return r.queryHoldingSnapshots(ctx, query, accountID, start, end)
}

func (r *holdingSnapshotRepository) queryHoldingSnapshots(ctx context.Context, query string, args ...any) ([]domain.HoldingSnapshot, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.HoldingSnapshot{}
	for rows.Next() {
		s := domain.HoldingSnapshot{}
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.AccountID, &s.UserAccountID, &s.SecurityID,
			&s.AsOfDate, &s.CapturedAt, &s.Present,
			&s.Quantity, &s.CostBasis, &s.InstitutionValue, &s.InstitutionPrice,
			&s.CurrencyCode, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return out, nil
}

// ── The read side (#1569): a closed account's retained positions ──────────────────────────────
//
// Both reads below are the HISTORICAL universe. They intentionally do NOT render
// currentStateAccountLeg / accountLiveOnDateSQL: the account whose history this answers for is
// typically terminal, and every liveness predicate in this package exists to keep such an account
// OUT of a current-state answer. Applying one here would return nothing for exactly the accounts
// the endpoint exists to serve.
//
// What they do carry is user_id, in SQL, on every statement.

// listHistoryDatesForAccountSQL — $1 = user_id, $2 = account_id.
//
// DISTINCT over an indexed (account_id, as_of_date) prefix. Unbounded by design and bounded in
// practice by elapsed days rather than sync frequency (one row per security per day, by the
// unique index), and the caller renders it as a day picker.
//
// ⚠ NO `AND hs.present` HERE, deliberately — see the interface doc. A day exists in this listing
// because a ROW was retained for it, not because a position was still held on it. Adding the
// filter back deletes a fully-liquidated day from the picker and makes the product deny retained
// data (pinned by TestHoldingHistoryRead_FullyLiquidatedDay_StaysInTheDateListingAndRendersEmpty).
const listHistoryDatesForAccountSQL = `
	SELECT DISTINCT hs.as_of_date
	FROM holding_snapshots hs
	WHERE hs.user_id = $1::uuid
	  AND hs.account_id = $2::uuid
	ORDER BY hs.as_of_date DESC
`

func (r *holdingSnapshotRepository) ListHistoryDatesForAccount(ctx context.Context, userID, accountID string) ([]string, error) {
	rows, err := r.db.Query(ctx, listHistoryDatesForAccountSQL, userID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		// Formatted at the edge of the database, once. A DATE comes back from pgx as a UTC-located
		// midnight; anything downstream that re-locates it can shift the day, and which day this
		// was is the entire content of the value.
		out = append(out, d.Format("2006-01-02"))
	}
	return out, rows.Err()
}

// getHistoryForAccountOnDateSQL — $1 = user_id, $2 = account_id, $3 = as_of_date.
//
// LEFT JOIN securities, matching the live holdings read's (GetByUserIDWithSecurities) LEFT JOIN
// securities shape — that is the only aspect this comment claims to match. ⚠ Since #1569 T8
// (Issue #1616) GetByUserIDWithSecurities also gates its accounts JOIN to a.type = 'investment';
// this query has no accounts JOIN at all (it is looked up by account_id directly, already scoped
// by its caller), so it is UNGATED by type. A depository account's history rows are still
// readable here even though the live read would now exclude that account's current holdings.
// That gap is accepted, not fixed by this task — see the reasoning at portfolio_snapshot_repository.go's
// Upsert, which the same trade-off (a small live-vs-history disagreement being less bad than the
// alternative) applies to.
//
// It is a LEFT join for CONSISTENCY, not for reachability: security_id is NOT NULL under a VALIDATED FK to
// securities with ON DELETE RESTRICT (migration 000160), so the matching row provably exists and
// INNER would return the same rows today. A mutant swapping it to INNER was checked and found to
// change nothing, which is why no test pins it — do not add one that pretends otherwise. What can
// still be NULL is the metadata itself: securities.ticker_symbol and securities.type, which is why
// those land in pointer fields.
//
// close_price is deliberately NOT selected. It is today's prior close, and there is no field on
// HoldingSnapshotView for it to reach — see the type's doc comment.
const getHistoryForAccountOnDateSQL = `
	SELECT
		hs.security_id, hs.as_of_date,
		hs.quantity, hs.cost_basis, hs.institution_value, hs.institution_price,
		hs.currency_code,
		sec.ticker_symbol, sec.name, sec.type
	FROM holding_snapshots hs
	LEFT JOIN securities sec ON sec.id = hs.security_id
	WHERE hs.user_id = $1::uuid
	  AND hs.account_id = $2::uuid
	  AND hs.as_of_date = $3::date
	  AND hs.present
	ORDER BY sec.type NULLS LAST, hs.institution_value DESC NULLS LAST, hs.security_id
`

func (r *holdingSnapshotRepository) GetHistoryForAccountOnDate(ctx context.Context, userID, accountID, asOf string) ([]domain.HoldingSnapshotView, error) {
	rows, err := r.db.Query(ctx, getHistoryForAccountOnDateSQL, userID, accountID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.HoldingSnapshotView{}
	for rows.Next() {
		v := domain.HoldingSnapshotView{}
		var day time.Time
		if err := rows.Scan(
			&v.SecurityID, &day,
			&v.Quantity, &v.CostBasis, &v.InstitutionValue, &v.InstitutionPrice,
			&v.CurrencyCode,
			&v.TickerSymbol, &v.SecurityName, &v.SecurityType,
		); err != nil {
			return nil, err
		}
		v.AsOfDate = day.Format("2006-01-02")
		v.ComputeGainLoss()
		out = append(out, v)
	}
	return out, rows.Err()
}
