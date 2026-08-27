package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

var (
	// ErrSnapshotNotFound is returned when a balance snapshot is not found in the database
	ErrSnapshotNotFound = errors.New("balance snapshot not found")
)

// BalanceSnapshotRepository defines the interface for balance snapshot data access
type BalanceSnapshotRepository interface {
	// Create inserts a new balance snapshot.
	// Does NOT perform upsert - allows multiple snapshots per account per day for high-frequency syncs.
	// Deduplication (5-minute window) should be handled by the service layer using GetLatestByAccountID.
	//
	// #1537 T93(b): the plain INSERT is deliberate and there is NO safe uniqueness key to add an
	// ON CONFLICT against. Investigated and rejected:
	//   - (account_id, snapshot_at, source): snapshot_at is a fresh time.Now().UTC() stamped per
	//     sync run (transactionSyncService.createBalanceSnapshots), so two runs never collide on
	//     it. A unique index here would never fire and would therefore dedup nothing.
	//   - (user_account_id, snapshot_at, source): same problem, plus user_account_id is NULLABLE
	//     (migration 000038 adds it with ON DELETE SET NULL), and NULLs never conflict under a
	//     plain UNIQUE — so every snapshot for an account with no user_account link would slip past.
	//   - (account_id, DATE(snapshot_at)): this is exactly the UNIQUE(account_id, snapshot_date)
	//     that migration 000017 DROPPED on purpose, to support multiple snapshots per day.
	// ⚠ Two claims here were CORRECTED in round 16 after adversarial review.
	//
	// (a) "No safe key exists" was too absolute. None exists in the CURRENT schema — there is no
	// stable logical run or event identifier on a snapshot — but a safe design does: persist a
	// sync_run_id (or the Plaid webhook/event id, which PlaidSyncArgs already carries as an
	// optional WebhookID but does not propagate here) and make it UNIQUE with (account_id, source).
	// That is the fix if this ever needs to be idempotent; see issue #1570.
	//
	// (b) "Retry duplication is bounded by the service's 5-minute window" is FALSE. The service
	// does GetLatestByAccountID and Create as two separate operations, and webhook processing does
	// not take the linked-item lock the sync worker uses — so two concurrent workers can both see
	// no recent snapshot and both insert. The window reduces duplication; it does not bound it.
	//
	// What remains true, and is why this is not urgent: every read path takes the latest snapshot
	// per account (per day for the aggregated-history reads), so extra rows inflate row count and
	// never a reported balance.
	Create(ctx context.Context, snapshot *domain.BalanceSnapshot) error

	// GetAggregatedHistory retrieves aggregated balance history for dashboard charts.
	// Returns daily total balance sums (grouped by DATE(snapshot_at)), optionally filtered by account types.
	// If accountTypes is nil or empty, all account types are included.
	GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error)

	// GetLatestByUserID retrieves the most recent balance snapshot for each account for a user.
	// Uses DISTINCT ON (account_id) to get only the latest snapshot per account.
	GetLatestByUserID(ctx context.Context, userID string) ([]domain.BalanceSnapshot, error)

	// GetSnapshotsAsOfForUser retrieves, for each of the user's logical accounts, the latest
	// snapshot at or before asOf. It is GetLatestByUserID bounded by a cutoff: the same
	// DISTINCT ON re-link chain key, plus `snapshot_at <= asOf`.
	//
	// This is the personal-scope twin of GetSnapshotsAsOfForHousehold, and it exists because a
	// point-in-time question ("what was the balance as of date D?") cannot be answered with a
	// date RANGE read. GetByUserIDAndDateRange(D, D) is a zero-width window that only matches a
	// snapshot stored at EXACTLY midnight UTC, and production stores wall-clock instants — so the
	// personal 30-day comparison it used to back never produced a result outside tests. Issue #1537.
	GetSnapshotsAsOfForUser(ctx context.Context, userID string, asOf time.Time) ([]domain.BalanceSnapshot, error)

	// GetSnapshotsAsOfForHousehold retrieves, for every account currently shared into the
	// household (across ALL members, not just requestingUserID), the latest snapshot at or
	// before asOf. Used for both the current point-in-time balance (asOf=now) and a
	// previous-period comparison (asOf=now-30d) — household mode must never fall back to a
	// personal-scope read (GetByUserIDAndDateRange, since DELETED in #1537), which dropped
	// (Codex adversarial review, P0-3). Joint accounts dedup to the freshest-synced copy.
	// Issue #1369 — Phase 5.
	GetSnapshotsAsOfForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.BalanceSnapshot, error)

	// GetAggregatedHistoryForHousehold is the household-scope variant of
	// GetAggregatedHistory: aggregates across every account shared into the household,
	// clipped per-account to share_started_at. Issue #1369 — Phase 5.
	GetAggregatedHistoryForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error)

	// GetLatestByAccountID retrieves the most recent balance snapshot for a specific account.
	// Used for deduplication checking (5-minute window) before creating new snapshots.
	GetLatestByAccountID(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error)

	// GetAccountBalanceHistory returns ONE account's retained day-by-day balances, oldest day
	// first — the reader privacy policy §8.2a's "balance history ... so you can still view it"
	// clause needs, and which did not exist before Issue #1569 (this round).
	//
	// ⚠ HISTORICAL UNIVERSE. It deliberately renders NO currentStateAccountLeg and no
	// status = 'active' predicate. Those exist to keep a terminal account OUT of a current-state
	// answer; applying one here would return nothing for exactly the closed accounts this method
	// exists to serve. It does not join `accounts` at all.
	//
	// ⚠ Tenancy is in SQL, on the one statement: user_id AND account_id. An account belonging to
	// somebody else has no history from this user's seat and returns the same empty series as an
	// account with none, so no existence is disclosed either way.
	//
	// start/end are inclusive UTC calendar-day bounds; either may be nil for "unbounded".
	GetAccountBalanceHistory(ctx context.Context, userID, accountID string, start, end *time.Time) ([]domain.AccountBalanceHistoryPoint, error)
}

// balanceSnapshotRepository implements BalanceSnapshotRepository using PostgreSQL
type balanceSnapshotRepository struct {
	db DB
}

// NewBalanceSnapshotRepository creates a new BalanceSnapshotRepository instance
func NewBalanceSnapshotRepository(db DB) BalanceSnapshotRepository {
	return &balanceSnapshotRepository{db: db}
}

// Create inserts a new balance snapshot
// Issue #171: Includes user_account_id for stable reference across Plaid re-links
func (r *balanceSnapshotRepository) Create(ctx context.Context, snapshot *domain.BalanceSnapshot) error {
	// Generate UUID if not already set
	if snapshot.ID == "" {
		snapshot.ID = uuid.New().String()
	}

	// Set created timestamp
	now := time.Now()
	snapshot.CreatedAt = now

	query := `
		INSERT INTO balance_snapshots
		(id, account_id, user_account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.Exec(ctx, query,
		snapshot.ID,
		snapshot.AccountID,
		snapshot.UserAccountID,
		snapshot.UserID,
		snapshot.CurrentBalance,
		snapshot.AvailableBalance,
		snapshot.CurrencyCode,
		snapshot.SnapshotAt,
		snapshot.Source,
		snapshot.CreatedAt,
	)

	return err
}

// aggregatedHistorySQLBase is GetAggregatedHistory's statement with ONE %s hole for the optional
// account-type predicate. It is a package-level var rather than an inline literal so the pgxmock
// tests can render the same string the production path renders — a hand-copied duplicate of a
// 90-line statement is a maintenance trap and told us nothing about correctness that the
// integration tests do not tell us better.
var aggregatedHistorySQLBase = `
		WITH RECURSIVE per_account_per_day AS (
		    SELECT DISTINCT ON ((bs.snapshot_at AT TIME ZONE 'UTC')::date, ` + snapshotIdentityKey + `)
		        (bs.snapshot_at AT TIME ZONE 'UTC')::date AS snapshot_date,
		        bs.account_id,
		        bs.current_balance,
		        a.type AS account_type
		    FROM balance_snapshots bs
		    LEFT JOIN accounts a ON a.id = bs.account_id
		    WHERE bs.user_id = $1
		      AND bs.snapshot_at >= $2
		      AND bs.snapshot_at <= $3
		      %s
		    ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date, ` + snapshotIdentityKey + `,
		             bs.snapshot_at DESC, bs.id DESC
		),
		live_all AS (
		    SELECT p.snapshot_date, a.id AS account_id, a.user_id AS owner_id,
		           a.user_account_id, a.linked_item_id
		    FROM per_account_per_day p
		    JOIN accounts a ON a.id = p.account_id
		),` + accountChainSupersessionCTESQL("TRUE") + `
		SELECT p.snapshot_date,
		       COALESCE(p.account_type, '') AS account_type,
		       SUM(p.current_balance) AS type_balance
		FROM per_account_per_day p
		WHERE NOT EXISTS (
		    SELECT 1 FROM superseded x
		    WHERE x.snapshot_date = p.snapshot_date
		      AND x.account_id = p.account_id
		)
		GROUP BY p.snapshot_date, p.account_type
		ORDER BY p.snapshot_date ASC, account_type ASC
	`

// GetAggregatedHistory retrieves aggregated balance history for dashboard charts.
// The query LEFT JOINs accounts unconditionally so that per-type breakdowns are always
// available for net-worth computation. Orphaned snapshots (deleted account rows) are
// included in TotalBalance but omitted from ByType.
func (r *balanceSnapshotRepository) GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	// Canary counter: every call applies the re-link chain CTE dedup (Issue #1109).
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()

	// Base query with parameterized account-type filter in CTE.
	// If accountTypes is provided, add AND a.type = ANY($4) to the WHERE clause.
	// #1569 task 19: this used to resolve the winning row per (day, chain) with
	// MAX(snapshot_at) AND MAX(id::text) computed INDEPENDENTLY, then join back on both. When a day
	// held two snapshots for one re-link chain — exactly what an institution-change overlap day
	// looks like — and the EARLIER row happened to have the higher uuid, no row satisfied both
	// conditions and the whole day DISAPPEARED from the user's balance history. With random uuids
	// that is a coin flip per overlap day. DISTINCT ON picks one real row, which is what was meant,
	// and is the same shape GetAggregatedHistoryForHousehold already used. Pinned by
	// TestHistoryRetentionRoundTrip_InstitutionChangeKeepsLastMonthWithoutDoubleCounting, whose
	// fixture chooses the uuids so the failing direction is deterministic.
	//
	// #1569 T10.2: the dedup grain moved from the re-link CHAIN to the PHYSICAL ACCOUNT
	// (snapshotIdentityKey), and the chain collapse is now done by the recorded-succession rule
	// instead. Keying on the chain deleted one of two maskless accounts from every day of the
	// chart — see snapshotIdentityKey and accountChainSupersessionCTE. The re-link direction is
	// unchanged and still pinned by
	// TestHistoryRetentionRoundTrip_InstitutionChangeKeepsLastMonthWithoutDoubleCounting.
	queryBase := aggregatedHistorySQLBase

	var query string
	var args []interface{}

	if len(accountTypes) > 0 {
		query = fmt.Sprintf(queryBase, "AND a.type = ANY($4)")
		args = []interface{}{userID, startDate, endDate, accountTypes}
	} else {
		query = fmt.Sprintf(queryBase, "")
		args = []interface{}{userID, startDate, endDate}
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []domain.BalanceHistoryPoint
	var current *domain.BalanceHistoryPoint
	for rows.Next() {
		var d time.Time
		var accountType string
		var bal float64
		if err := rows.Scan(&d, &accountType, &bal); err != nil {
			return nil, err
		}
		if current == nil || !current.Date.Equal(d) {
			history = append(history, domain.BalanceHistoryPoint{Date: d, ByType: map[string]float64{}})
			current = &history[len(history)-1]
		}
		current.TotalBalance += bal
		if accountType != "" {
			current.ByType[accountType] += bal
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return history, nil
}

// ── The personal point-in-time reads (#1569 T10.2) ────────────────────────────────────────────
//
// snapshotIdentityKey is the grain these two reads collapse to. It is ONE PHYSICAL ACCOUNT, not
// one re-link chain, and that change is the whole of the maskless-pair fix.
//
// Both reads used to say DISTINCT ON (COALESCE(user_account_id, account_id)) — one row per CHAIN.
// Two maskless accounts at one institution are FORCED to share a user_accounts row (migration
// 000100's `UNIQUE (user_id, institution_id_blind_idx, mask_blind_idx) NULLS NOT DISTINCT`, plus
// FindByInstitutionAndMask's `mask_blind_idx IS NULL` branch), so a brokerage and an IRA at one
// broker are one chain key with two live accounts, and the chain rule DELETED one of them from
// every total. Measured, not reasoned: $100 + $400 reported as $400
// (TestMasklessPair_PersonalBalanceReads_CountBothAccounts).
//
// ⚠ THE THREE COALESCE ARMS ARE EACH LOAD-BEARING, and the third one is the trap.
// balance_snapshots.account_id is NULLABLE with ON DELETE SET NULL, so an orphaned snapshot is a
// real shape, not a theoretical one. A bare DISTINCT ON (account_id) puts EVERY orphan in ONE
// group (SQL NULL is a single DISTINCT ON bucket) and silently deletes all but one orphaned
// account's history — a bigger error than the one being fixed. So: the physical account when
// there is one, the logical account when the physical row is gone, and a single shared bucket
// when neither exists, which is exactly what the pre-fix key did for that last case.
const snapshotIdentityKey = `COALESCE(bs.account_id::text, bs.user_account_id::text, '')`

// personalLatestSnapshotSQL renders the shared body of GetLatestByUserID (no cutoff) and
// GetSnapshotsAsOfForUser (cutoff at $2). They are one statement with one optional predicate
// BECAUSE they must agree about which row wins: the two used to be copy-and-paste siblings and
// the whole point of GetSnapshotsAsOfForUser's doc comment is that they do not drift.
//
// ⚠ WITH RECURSIVE is required, not stylistic: accountChainSupersessionCTE walks
// linked_items.replaced_by_id transitively and Postgres takes RECURSIVE from the WITH keyword.
//
// ⚠ $OBSERVED is TRUE here, and that is not a shortcut. live_all is built FROM the winning
// snapshots, so an account is only in it because it reported a balance at or before the cutoff —
// membership already IS the observation the portfolio side has to ask for separately. A successor
// whose first sync lands after the cutoff is therefore absent, and its predecessor is correctly
// kept: that is the point-in-time answer.
func personalLatestSnapshotSQL(cutoffClause string) string {
	return `
		WITH RECURSIVE latest AS (
		    SELECT DISTINCT ON (` + snapshotIdentityKey + `)
		           bs.id, bs.account_id, bs.user_account_id, bs.user_id, bs.current_balance,
		           bs.available_balance, bs.currency_code, bs.snapshot_at, bs.source, bs.created_at
		    FROM balance_snapshots bs
		    WHERE bs.user_id = $1
		      ` + cutoffClause + `
		    -- bs.id DESC is #1569 T10.2 and is independently correct even without the rest of this
		    -- fix: createBalanceSnapshots stamps ONE time.Now() for the whole loop, so two rows can
		    -- carry an identical snapshot_at and, with no id term, which one wins was free to FLIP
		    -- between successive requests.
		    ORDER BY ` + snapshotIdentityKey + `, bs.snapshot_at DESC, bs.id DESC
		),
		live_all AS (
		    SELECT DATE '0001-01-01' AS snapshot_date, a.id AS account_id, a.user_id AS owner_id,
		           a.user_account_id, a.linked_item_id
		    FROM accounts a
		    JOIN latest l ON l.account_id = a.id
		),` + accountChainSupersessionCTESQL("TRUE") + `
		SELECT l.id, l.account_id, l.user_account_id, l.user_id, l.current_balance,
		       l.available_balance, l.currency_code, l.snapshot_at, l.source, l.created_at
		FROM latest l
		-- NOT EXISTS against superseded rather than a JOIN to the live CTE: an orphaned snapshot has a
		-- NULL account_id, joins no accounts row, and must still be returned. A join would drop it.
		WHERE NOT EXISTS (
		    SELECT 1 FROM superseded x WHERE x.account_id = l.account_id
		)
	`
}

// GetLatestByUserID retrieves the most recent balance snapshot for each of the user's physical
// accounts, minus those a recorded successor superseded. See personalLatestSnapshotSQL.
func (r *balanceSnapshotRepository) GetLatestByUserID(ctx context.Context, userID string) ([]domain.BalanceSnapshot, error) {
	// Canary counter: every call applies the re-link succession rule (Issue #1109).
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()
	return r.scanSnapshots(ctx, personalLatestSnapshotSQL(""), userID)
}

// GetSnapshotsAsOfForUser retrieves the latest snapshot at or before asOf for each of the user's
// physical accounts. Mirrors GetLatestByUserID exactly — same statement, same tie-break — with the
// single addition of `snapshot_at <= $2`, so the two agree on which row wins when asOf is in the
// future. Issue #1537.
func (r *balanceSnapshotRepository) GetSnapshotsAsOfForUser(ctx context.Context, userID string, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	// Canary counter: every call applies the re-link succession rule (Issue #1109).
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()
	return r.scanSnapshots(ctx, personalLatestSnapshotSQL("AND bs.snapshot_at <= $2"), userID, asOf)
}

// GetSnapshotsAsOfForHousehold retrieves the latest snapshot at or before asOf for every
// account shared into the household, across all members. Joint accounts (same physical
// fingerprint shared by two members) dedup via householdAccountsCTEForBalancesAsOf, which picks
// the freshest-synced copy — then DISTINCT ON here also collapses that chosen
// user_account_id to its single latest-as-of-asOf snapshot. Issue #1369 — Phase 5.
func (r *balanceSnapshotRepository) GetSnapshotsAsOfForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()

	query := "WITH " + householdAccountsCTEForBalancesAsOf() + `
		SELECT DISTINCT ON (bs.user_account_id)
		       bs.id, bs.account_id, bs.user_account_id, bs.user_id, bs.current_balance, bs.available_balance,
		       bs.currency_code, bs.snapshot_at, bs.source, bs.created_at
		FROM balance_snapshots bs
		JOIN household_accounts ha ON ha.user_account_id = bs.user_account_id
		WHERE bs.snapshot_at <= $4
		ORDER BY bs.user_account_id, bs.snapshot_at DESC
	`

	args := householdScopeArgs(requestingUserID, scope)
	args = append(args, asOf)
	return r.scanSnapshots(ctx, query, args...)
}

// GetAggregatedHistoryForHousehold is the household-scope variant of GetAggregatedHistory:
// aggregates across every account shared into the household, clipped per-account to
// share_started_at.
//
// Joint-account fingerprint collisions (the same physical account linked and shared by two
// members) are resolved PER CALENDAR DAY, not once for the whole query: latest_per_account_per_day
// uses DISTINCT ON ((day), fingerprint) to pick each day's freshest-synced copy independently.
// This matters because a joint account's globally-freshest copy may belong to a member who only
// started syncing partway through [startDate, endDate] — resolving one query-wide winner (as an
// earlier version of this method did, via householdAccountsCTEForBalances) would erase the OTHER
// member's genuinely-earlier history from the ENTIRE series, not merely clip it at
// share_started_at (Codex adversarial review P1-2; fixed here per #1378, tracked as a fast-follow
// from #1369 — the asOf-bounded point-in-time case, householdAccountsCTEForBalancesAsOf, was
// already correct since it only ever resolves a single instant).
//
// The accountTypes filter is applied to the per-day WINNER's type in the outer query, not inside
// the DISTINCT ON candidate set — filtering candidates first could let a stale copy whose type
// happens to match beat the freshest copy for a day.
//
// Pre-existing, unchanged-by-this-fix assumption: current_balance always comes from the
// selected balance_snapshots row directly (never wrong), but account_type comes from
// whichever `accounts` row the LEFT JOIN matches by user_account_id — not specifically
// bs.account_id. The ORDER BY prefers the ACTIVE chain member's type and falls back to any
// member's type when the whole chain has been expired or replaced; the choice is only
// ambiguous if two 'active' accounts rows ever shared one user_account_id, which the Plaid
// re-link handler guarantees never happens (no DB constraint enforces it — see migration
// 000033). The trailing `a.id` ORDER BY tiebreak keeps that (should-be-impossible) case
// deterministic rather than fanning out and double-summing balances, as the pre-#1378
// query did.
func (r *balanceSnapshotRepository) GetAggregatedHistoryForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()

	const queryBase = `
		WITH ` + `%s,` + `
		per_account_per_day AS (
		    -- #1569 T7, stage 1 of 2: collapse to ONE row per (day, user_account_id), applying the
		    -- grant INTERVAL clip that makes a departed member's shared period survive their
		    -- departure without leaking a single day past it.
		    --
		    -- This stage is not optional bookkeeping. household_accounts now emits one row per grant
		    -- INTERVAL, and a user_account can match several on the same day (closed intervals are
		    -- unconstrained by uq_household_account_shares_active and may overlap). The stage-2 dedup
		    -- below keys on the fingerprint, which a re-fingerprinted account changes between
		    -- intervals — so without this collapse ONE account lands in two buckets and its balance is
		    -- summed TWICE into that day's total. Pinned by
		    -- TestHouseholdShareRange_OverlappingRefingerprintedIntervalsDoNotDoubleCount.
		    --
		    -- The interval in force wins (share_started_at DESC, share_row_id), matching
		    -- householdAccountsCTEForBalancesAsOf's rule, so the fingerprint carried into stage 2 is
		    -- the one that was actually recorded on that day.
		    SELECT DISTINCT ON ((bs.snapshot_at AT TIME ZONE 'UTC')::date, ha.user_account_id)
		        (bs.snapshot_at AT TIME ZONE 'UTC')::date AS snapshot_date,
		        ha.user_account_id,
		        ha.fingerprint,
		        bs.snapshot_at,
		        bs.id AS snapshot_id,
		        bs.current_balance,
		        a.type AS account_type
		    FROM balance_snapshots bs
		    JOIN household_accounts ha ON ha.user_account_id = bs.user_account_id
		    -- This join exists solely to supply a.type for the per-type grouping; the balance itself
		    -- always comes from the balance_snapshots row. It matches EVERY member of a re-link chain
		    -- sharing that user_account_id, so type selection has to be an explicit PREFERENCE, not a
		    -- filter:
		    --
		    --   * #1561 removed AND a.status = 'active' from the household ANALYTICS joins, correctly
		    --     — there it was dropping pre-relink history. Applying the same removal here was wrong
		    --     (restored in 58920ae3): with no preference, DISTINCT ON breaks the tie on a.id (an
		    --     arbitrary UUID) and can attribute the balance to a REPLACED member's type.
		    --   * But expressing that preference as a JOIN predicate was ALSO wrong. When no active row
		    --     shares the user_account_id — exactly what ExpireAccountsByFingerprint leaves behind
		    --     for a whole fingerprint — the LEFT JOIN yields a NULL a.type, which lands in the ''
		    --     bucket and, because NULL never satisfies = ANY, is dropped outright by the
		    --     accountTypes filter. Personal scope keeps those snapshots (it reads the type from the
		    --     snapshot's own account row), so household history silently disagreed with it.
		    --
		    -- So the join is unfiltered and the ORDER BY prefers an active row, falling back to any
		    -- chain member's type when none is active. Both properties are pinned:
		    -- TestGetAggregatedHistoryForHousehold_RelinkChain_UsesActiveAccountType (active wins) and
		    -- TestGetAggregatedHistoryForHousehold_ExpiredOnlyChain_KeepsTypeFilteredHistory (fallback).
		    LEFT JOIN accounts a ON a.user_account_id = bs.user_account_id
		    -- The caller's requested window, at date resolution because that is what it is: a
		    -- range of calendar days the chart covers.
		    WHERE (bs.snapshot_at AT TIME ZONE 'UTC')::date >= $4::date
		      AND (bs.snapshot_at AT TIME ZONE 'UTC')::date <= $5::date
		      -- The GRANT's own bounds, at INSTANT resolution — [share_started_at, share_ended_at),
		      -- the same half-open interval householdAccountsCTEForBalancesAsOf evaluates, evaluated
		      -- the same way. Comparing bs.snapshot_at directly is what lets both bounds be exact:
		      -- the household keeps every instant it was genuinely entitled to, including ones
		      -- earlier on the day it gained or lost access, and gets nothing outside them.
		      --
		      -- Both bounds used to be date-resolution, and each was wrong in its own direction. The
		      -- opening bound was GREATEST($4::date, share_started_at::date), which admitted the
		      -- WHOLE share-start day: a snapshot taken hours BEFORE the grant existed entered the
		      -- series. The closing bound excluded the revocation day whole, which leaked nothing but
		      -- threw away real history on a false binary — "drop the last partial day OR admit a
		      -- post-departure instant" was never the choice. Pinned in both directions by
		      -- TestHouseholdShareRange_ShareStartDayExcludesPreGrantInstants and
		      -- TestHouseholdShareRange_RevocationDayKeepsOnlyPreRevocationInstants.
		      AND bs.snapshot_at >= ha.share_started_at
		      AND (ha.share_ended_at IS NULL OR bs.snapshot_at < ha.share_ended_at)
		    ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date, ha.user_account_id,
		             ha.share_started_at DESC, ha.share_row_id,
		             bs.snapshot_at DESC, bs.id DESC,
		             (a.status = 'active') DESC NULLS LAST, a.id
		),
		latest_per_account_per_day AS (
		    -- Stage 2, unchanged in intent: resolve joint-account fingerprint collisions PER DAY, the
		    -- freshest-synced copy winning. Its input is already one row per (day, user_account), so
		    -- the snapshot-recency ordering picks the same winner it always did; the trailing
		    -- user_account_id keeps a full tie deterministic rather than arbitrary.
		    SELECT DISTINCT ON (snapshot_date, fingerprint)
		        snapshot_date,
		        current_balance,
		        account_type
		    FROM per_account_per_day
		    ORDER BY snapshot_date, fingerprint, snapshot_at DESC, snapshot_id DESC, user_account_id
		)
		SELECT snapshot_date,
		       COALESCE(account_type, '') AS account_type,
		       SUM(current_balance) AS type_balance
		FROM latest_per_account_per_day
		%s
		GROUP BY snapshot_date, account_type
		ORDER BY snapshot_date ASC, account_type ASC
	`

	args := householdScopeArgs(requestingUserID, scope)
	args = append(args, startDate, endDate)

	var query string
	if len(accountTypes) > 0 {
		args = append(args, accountTypes)
		query = fmt.Sprintf(queryBase, householdAccountsCTEForBalancesPerDay(), "WHERE account_type = ANY($6)")
	} else {
		query = fmt.Sprintf(queryBase, householdAccountsCTEForBalancesPerDay(), "")
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []domain.BalanceHistoryPoint
	var current *domain.BalanceHistoryPoint
	for rows.Next() {
		var d time.Time
		var accountType string
		var bal float64
		if err := rows.Scan(&d, &accountType, &bal); err != nil {
			return nil, err
		}
		if current == nil || !current.Date.Equal(d) {
			history = append(history, domain.BalanceHistoryPoint{Date: d, ByType: map[string]float64{}})
			current = &history[len(history)-1]
		}
		current.TotalBalance += bal
		if accountType != "" {
			current.ByType[accountType] += bal
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return history, nil
}

// ── Per-account retained balance history (#1569) ──────────────────────────────────────────────

// getAccountBalanceHistorySQL — $1 = user_id, $2 = account_id, $3 = start day or NULL,
// $4 = end day or NULL.
//
// ONE point per UTC day. balance_snapshots deliberately carries no per-day uniqueness key (see
// Create's doc comment), so a busy day holds several rows; DISTINCT ON collapses them with the
// SAME tie-break GetAggregatedHistory's second stage uses — freshest snapshot_at, then id — so the
// per-account series and the aggregated dashboard series can never disagree about which row is a
// day's answer.
//
// The day is derived as (snapshot_at AT TIME ZONE 'UTC')::date, matching
// idx_balance_snapshots_date_trunc and every other daily read in this file. A bare ::date cast
// would follow the session TimeZone and silently re-bucket rows around midnight.
//
// The bounds are written as `$3::date IS NULL OR ...` so one statement serves the bounded and the
// unbounded case; passing a typed nil *time.Time binds SQL NULL.
const getAccountBalanceHistorySQL = `
	SELECT DISTINCT ON ((bs.snapshot_at AT TIME ZONE 'UTC')::date)
	       (bs.snapshot_at AT TIME ZONE 'UTC')::date AS as_of_date,
	       bs.current_balance,
	       bs.available_balance,
	       bs.currency_code
	FROM balance_snapshots bs
	WHERE bs.user_id = $1::uuid
	  AND bs.account_id = $2::uuid
	  AND ($3::date IS NULL OR (bs.snapshot_at AT TIME ZONE 'UTC')::date >= $3::date)
	  AND ($4::date IS NULL OR (bs.snapshot_at AT TIME ZONE 'UTC')::date <= $4::date)
	ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date ASC, bs.snapshot_at DESC, bs.id DESC
`

func (r *balanceSnapshotRepository) GetAccountBalanceHistory(
	ctx context.Context, userID, accountID string, start, end *time.Time,
) ([]domain.AccountBalanceHistoryPoint, error) {
	rows, err := r.db.Query(ctx, getAccountBalanceHistorySQL, userID, accountID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.AccountBalanceHistoryPoint{}
	for rows.Next() {
		var day time.Time
		p := domain.AccountBalanceHistoryPoint{}
		if err := rows.Scan(&day, &p.CurrentBalance, &p.AvailableBalance, &p.CurrencyCode); err != nil {
			return nil, err
		}
		// Formatted at the edge of the database, once. A DATE comes back from pgx as a
		// UTC-located midnight; anything downstream that re-locates it can shift the day, and
		// which day this was is the entire meaning of the figure beside it.
		p.Date = day.Format("2006-01-02")
		out = append(out, p)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return out, nil
}

// GetLatestByAccountID retrieves the most recent balance snapshot for a specific account
func (r *balanceSnapshotRepository) GetLatestByAccountID(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error) {
	query := `
		SELECT id, account_id, user_account_id, user_id, current_balance, available_balance,
		       currency_code, snapshot_at, source, created_at
		FROM balance_snapshots
		WHERE account_id = $1
		ORDER BY snapshot_at DESC
		LIMIT 1
	`

	var s domain.BalanceSnapshot
	err := r.db.QueryRow(ctx, query, accountID).Scan(
		&s.ID,
		&s.AccountID,
		&s.UserAccountID,
		&s.UserID,
		&s.CurrentBalance,
		&s.AvailableBalance,
		&s.CurrencyCode,
		&s.SnapshotAt,
		&s.Source,
		&s.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // No snapshot found - return nil, nil (not an error)
	}
	if err != nil {
		return nil, err
	}

	return &s, nil
}

// scanSnapshots is a helper to reduce duplication in snapshot scanning logic
func (r *balanceSnapshotRepository) scanSnapshots(ctx context.Context, query string, args ...interface{}) ([]domain.BalanceSnapshot, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []domain.BalanceSnapshot
	for rows.Next() {
		var s domain.BalanceSnapshot
		err := rows.Scan(
			&s.ID,
			&s.AccountID,
			&s.UserAccountID,
			&s.UserID,
			&s.CurrentBalance,
			&s.AvailableBalance,
			&s.CurrencyCode,
			&s.SnapshotAt,
			&s.Source,
			&s.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return snapshots, nil
}
