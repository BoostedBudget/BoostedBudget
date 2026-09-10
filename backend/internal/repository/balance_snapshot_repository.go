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

	// CreateDailyCapture inserts a snapshot written by the periodic gap-filler (#1626 Phase 3,
	// Defect 3) and reports whether it actually inserted.
	//
	// It is a SEPARATE method from Create rather than a flag on it, for two reasons:
	//
	//  1. Conflict handling. This is the ONE provenance for which per-(account, UTC day)
	//     uniqueness is safe — the gap-filler is once-daily by construction and every read
	//     collapses to latest-per-account-per-day, so a second same-day row from it could not
	//     surface anywhere. Migration 000194's partial unique index says exactly that, and this
	//     statement's ON CONFLICT names that index's key AND its predicate, so it can never
	//     silently swallow any other conflict (a duplicate primary key included). Create keeps its
	//     plain INSERT, so webhook history keeps the several-rows-per-day freedom migration 000017
	//     deliberately restored.
	//  2. Cache classification. Create's cache disposition rests on "the sync is its sole caller".
	//     A new writer must not quietly falsify that premise inside an existing entry; it gets its
	//     own entry, with its own argument. See cache_completeness_repo_test.go.
	//
	// The boolean is not decoration. The job needs to distinguish "I filled the gap" from
	// "somebody already had", and a conflict here is a NORMAL outcome (two capture jobs racing),
	// not an error — which is why it is reported rather than returned. Every OTHER failure is
	// returned, because the capture path must fail loudly: the writer this replaces logged and
	// swallowed individual insert failures while reporting success, which is exactly how a partial
	// cohort came to commit with River recording success.
	//
	// ⚠ SQLSTATE 42P10 here means migration 000194's index is missing, not that the caller did
	// anything wrong. That is deliberate: a missing idempotency key should be loud.
	CreateDailyCapture(ctx context.Context, snapshot *domain.BalanceSnapshot) (bool, error)

	// AccountsMissingDailySnapshot returns the IDs of linkedItemID's ACTIVE accounts that have no
	// balance snapshot at all on the given UTC calendar day. An empty result means the item's full
	// cohort is covered for that day.
	//
	// This is the question the periodic job's "skip this item, make no Plaid call" decision rests
	// on, and three of its details are load-bearing:
	//
	//   - FULL COHORT, not "any row today". A webhook that snapshotted 3 of an item's 5 accounts
	//     leaves the incomplete-cohort defect in place for that day (a day's aggregate used to be
	//     the sum of only the accounts that received a row), so a partial day must NOT count as
	//     done.
	//   - ANY source counts. The point is to make no provider call when a webhook has already
	//     covered the day; requiring a daily_capture row would call Plaid for every item every day.
	//   - ACTIVE accounts only. A removed account must accrue no snapshots — counting it would make
	//     its item report an incomplete cohort every day forever, so the item would be processed
	//     daily and the "zero calls for actively-syncing users" property would never hold.
	//
	// The day is a UTC CALENDAR day, compared with the same `(snapshot_at AT TIME ZONE 'UTC')::date`
	// expression every aggregated read groups by and migration 000194's index is built on. Any
	// instant within the intended day may be passed.
	AccountsMissingDailySnapshot(ctx context.Context, linkedItemID string, day time.Time) ([]string, error)
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

// createDailyCaptureSQL is CreateDailyCapture's statement.
//
// The ON CONFLICT target spells out BOTH halves of migration 000194's index — the (account_id, UTC
// day) key and the `WHERE source = 'daily_capture'` predicate — because Postgres infers an arbiter
// index by matching them. Two consequences, both wanted:
//
//   - it can only ever swallow a conflict on THAT index, so a duplicate primary key or any future
//     constraint still errors out loudly. A bare `ON CONFLICT DO NOTHING` would swallow all of
//     them, and this is a write path whose entire remit is to stop failing silently;
//   - if the index is missing, this fails with SQLSTATE 42P10 rather than degrading to a
//     duplicate-producing insert.
//
// The predicate must stay spelled the way the index spells it. A rewrite of either (say to
// `source <> 'plaid_webhook'`) stops Postgres proving the implication and the statement starts
// erroring, which is at least loud — but it is one edit away from wrong.
const createDailyCaptureSQL = `
		INSERT INTO balance_snapshots
		(id, account_id, user_account_id, user_id, current_balance, available_balance, currency_code, snapshot_at, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (account_id, ((snapshot_at AT TIME ZONE 'UTC')::date))
		    WHERE source = 'daily_capture'
		DO NOTHING
	`

// CreateDailyCapture — see the interface for the contract and for why it is not Create with a flag.
func (r *balanceSnapshotRepository) CreateDailyCapture(ctx context.Context, snapshot *domain.BalanceSnapshot) (bool, error) {
	if snapshot == nil {
		return false, errors.New("balance snapshot is nil")
	}
	// A caller that reached here with any other provenance would be inserting a row the ON CONFLICT
	// clause's arbiter cannot cover, so the uniqueness this method advertises would silently not
	// apply. Refuse rather than write an unconstrained row through the constrained door.
	if snapshot.Source != domain.SnapshotSourceDailyCapture {
		return false, fmt.Errorf("CreateDailyCapture requires source %q, got %q",
			domain.SnapshotSourceDailyCapture, snapshot.Source)
	}
	if snapshot.ID == "" {
		snapshot.ID = uuid.New().String()
	}
	snapshot.CreatedAt = time.Now()

	tag, err := r.db.Exec(ctx, createDailyCaptureSQL,
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
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// accountsMissingDailySnapshotSQL is AccountsMissingDailySnapshot's statement.
//
// NOT EXISTS rather than a LEFT JOIN ... IS NULL: the question is existence, so a semi-join cannot
// multiply an account by the several snapshots it may already have for the day (webhook history
// permits many, by design — migration 000017).
//
// The date comparison is on the account's snapshots, not on a range of instants, and it uses the
// same `(snapshot_at AT TIME ZONE 'UTC')::date` expression as migration 000017's
// idx_balance_snapshots_date_trunc and 000194's uniqueness key — one notion of "the day a snapshot
// belongs to" across the schema. Comparing instants here would reintroduce the bug #1626 Phase 3
// step 1f fixed on the read side, where a snapshot written today at any hour fell outside a range
// ending "today".
const accountsMissingDailySnapshotSQL = `
		SELECT a.id
		FROM accounts a
		WHERE a.linked_item_id = $1
		  AND a.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM balance_snapshots bs
		      WHERE bs.account_id = a.id
		        AND (bs.snapshot_at AT TIME ZONE 'UTC')::date = ($2::timestamptz AT TIME ZONE 'UTC')::date
		  )
		ORDER BY a.id
	`

// AccountsMissingDailySnapshot — see the interface for the contract and for which three details of
// it are load-bearing.
func (r *balanceSnapshotRepository) AccountsMissingDailySnapshot(ctx context.Context, linkedItemID string, day time.Time) ([]string, error) {
	rows, err := r.db.Query(ctx, accountsMissingDailySnapshotSQL, linkedItemID, day.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	missing := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		missing = append(missing, id)
	}
	return missing, rows.Err()
}

// aggregatedHistorySQLBase is GetAggregatedHistory's statement with ONE %s hole for the optional
// account-type predicate. It is a package-level var rather than an inline literal so the pgxmock
// tests can render the same string the production path renders — a hand-copied duplicate of a
// 90-line statement is a maintenance trap and told us nothing about correctness that the
// integration tests do not tell us better.
var aggregatedHistorySQLBase = `
		-- ── observed: the rows that EXIST, one per (UTC day, physical account) ────────────────
		--
		-- Unbounded BELOW on purpose. The caller's start date is applied at the very end, to the
		-- filled series, because a day at the START of the window has to be able to inherit an
		-- account's last balance from BEFORE the window — otherwise a "this month" chart would
		-- under-count every account whose most recent sync happened last month.
		WITH RECURSIVE observed AS (
		    SELECT DISTINCT ON ((bs.snapshot_at AT TIME ZONE 'UTC')::date, ` + snapshotIdentityKey + `)
		        (bs.snapshot_at AT TIME ZONE 'UTC')::date AS snapshot_date,
		        ` + snapshotIdentityKey + ` AS identity_key,
		        bs.account_id,
		        bs.current_balance,
		        -- An ORPHANED snapshot (accounts row gone, account_id SET NULL) still knows its
		        -- LOGICAL account, and user_accounts.identity_type is that account's recorded type
		        -- — immutable, enforced by trg_user_accounts_identity_immutable. Recovering it is
		        -- #1626 Phase 3 item 5: orphans counted toward TotalBalance but carried no ByType
		        -- entry, and net-worth history is reconstructed EXCLUSIVELY from ByType
		        -- (dashboard_service.go GetNetWorthHistory), so a day whose only rows were orphans
		        -- reported net worth of EXACTLY ZERO beside a correct balance. Falling back to an
		        -- untyped bucket would not have been enough either: domain.AccountTypeCategory
		        -- defaults to "assets", so an orphaned CREDIT CARD would have been reconstructed as
		        -- an asset and overstated net worth by twice its balance.
		        COALESCE(a.type, ua.identity_type) AS account_type
		    FROM balance_snapshots bs
		    LEFT JOIN accounts a ON a.id = bs.account_id
		    -- LEFT and on the snapshot's OWN chain pointer: this join exists only to recover a type
		    -- the accounts row can no longer supply, and user_accounts(id) is the PRIMARY KEY
		    -- (migration 000033) so it cannot multiply the row.
		    LEFT JOIN user_accounts ua ON ua.id = bs.user_account_id
		    WHERE bs.user_id = $1
		      -- #1626 Phase 3 step 1f: CALENDAR DATES, not instants, and the two scopes now agree.
		      -- This bound used to be "bs.snapshot_at <= $3" against an endDate that is MIDNIGHT UTC
		      -- by construction — dateutil.ParseDate turns YYYY-MM-DD into midnight and both
		      -- dashboard services (GetBalanceHistory, GetNetWorthHistory) pass that instant
		      -- straight through — so a snapshot written today at any hour fell OUTSIDE a range
		      -- ending "today" and today's data was invisible. The frontend then fabricated it by
		      -- forward-fill, which is why the chart looked plausible while being wrong.
		      --
		      -- The household twin GetAggregatedHistoryForHousehold already compared dates and was
		      -- already correct; matching it means ONE comparison style across both scopes rather
		      -- than two, and this is also the INDEXED form (migration 000017's
		      -- idx_balance_snapshots_date_trunc is on (user_id, ((snapshot_at AT TIME ZONE
		      -- 'UTC')::date)) — verified present in this schema). endOfDay() in dashboard_service.go
		      -- would also have worked and was rejected for exactly that reason: it would have left
		      -- the two scopes phrased differently and left the index unusable here.
		      AND (bs.snapshot_at AT TIME ZONE 'UTC')::date <= $3::date
		      %s
		    ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date, ` + snapshotIdentityKey + `,
		             bs.snapshot_at DESC, bs.id DESC
		),
		-- ── the grid the carry-forward fills ─────────────────────────────────────────────────
		--
		-- DAYS ARE NOT INVENTED, only VALUES. A chart point exists for a day only if some snapshot
		-- was actually taken on it; a day nobody synced at all is still absent from the series and
		-- the frontend still bridges it. That is deliberate and narrow: generating a full calendar
		-- grid would also generate rows for days in the FUTURE (endDate is unvalidated against
		-- today), turning a frontend-fabricated flat line into a backend-fabricated one. The future
		-- clamp is a separate change and this read must not pre-empt it.
		observed_days AS (
		    SELECT DISTINCT snapshot_date FROM observed
		),
		-- Every account this user has EVER reported at or before the window's end. Keyed on
		-- identity_key rather than account_id for the same reason the DISTINCT ON is — see
		-- snapshotIdentityKey, whose third COALESCE arm keeps orphans from collapsing into one.
		observed_keys AS (
		    SELECT DISTINCT identity_key FROM observed
		),
		grid AS (
		    SELECT d.snapshot_date, k.identity_key
		    FROM observed_days d
		    CROSS JOIN observed_keys k
		),
		-- carry_source resolves, for each (day, account), WHICH observed day supplies its balance:
		-- the latest one at or before this day. A running max() over the LEFT-JOINed grid is the
		-- LOCF idiom Postgres can express without IGNORE NULLS, and it is O(rows log rows) rather
		-- than the O(days x rows) a per-day LATERAL would cost on a multi-year window.
		--
		-- ⚠ THE JOIN EXCLUDES A NULL current_balance, AND THAT IS DECISION 4 OF THIS CHANGE.
		-- transactionSyncService accepts a snapshot with current_balance NULL and available_balance
		-- present (see its balance write), and this aggregation sums only current_balance — so such
		-- an account used to be silently dropped from the day's total, and on a day whose ONLY row
		-- was one of those, SUM() returned SQL NULL and the row scan failed outright with "cannot
		-- scan NULL into *float64", i.e. a 500 for the whole chart.
		--
		-- The two candidate fixes were COALESCE to available_balance, or exclude the row and let the
		-- carry-forward supply the account's previous real value. EXCLUDE was chosen: available
		-- balance is a DIFFERENT QUANTITY, not a degraded version of the same one. On a credit card
		-- it is the remaining credit line, so substituting it would report a large positive asset
		-- where a debt belongs. "Last known CURRENT balance" is the honest answer, and it is what
		-- LOCF already computes. The day itself still survives, because observed_days is built from
		-- observed rather than from this join.
		carry_source AS (
		    SELECT g.snapshot_date,
		           g.identity_key,
		           max(o.snapshot_date) OVER (
		               PARTITION BY g.identity_key ORDER BY g.snapshot_date
		               ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
		           ) AS source_date
		    FROM grid g
		    LEFT JOIN observed o
		      ON o.identity_key = g.identity_key
		     AND o.snapshot_date = g.snapshot_date
		     AND o.current_balance IS NOT NULL
		),
		-- ── per_account_per_day: the filled series, which is what the aggregate sums ──────────
		--
		-- THE DEFECT THIS CLOSES. This CTE used to BE the observed rows, so a day's total was the
		-- sum of only the accounts that happened to receive a snapshot that day. Snapshots are
		-- written per-ITEM on webhook sync, so a day on which one item synced reported only that
		-- item's accounts and the total collapsed toward zero — while every other account's balance
		-- was merely ABSENT from that date rather than gone. Every per-account balance is provider
		-- truth; the day-level AGGREGATE was not, and that distinction is the whole defect. Carrying
		-- forward also REPAIRS EXISTING history, which no amount of future snapshotting can.
		--
		-- Step 2b, decided here: history starts PER ACCOUNT, at that account's earliest snapshot.
		-- A key with no observed row at or before the day has a NULL source_date and this join
		-- drops it, so the total GROWS as accounts join rather than starting at the first day on
		-- which every account happens to have data. The alternative hides real history.
		per_account_per_day AS (
		    SELECT c.snapshot_date,
		           o.account_id,
		           o.current_balance,
		           o.account_type
		    FROM carry_source c
		    JOIN observed o
		      ON o.identity_key = c.identity_key
		     AND o.snapshot_date = c.source_date
		     AND o.current_balance IS NOT NULL
		    WHERE c.snapshot_date >= $2::date
		      -- ⚠ THE REMOVAL BOUND, AND IT APPLIES ONLY TO CARRIED ROWS. An account's OWN snapshot
		      -- day keeps its value unconditionally — that row is provider truth about a day the
		      -- account really reported, and re-asking account_status_history about it would delete
		      -- a closed account's retained history from the chart, which is the opposite of what
		      -- #1569 exists to do (see accountChainLiveAllCTE, which says so for the same reason).
		      -- A CARRIED row is different in kind: it is this query's inference that the balance
		      -- persisted, and that inference is only defensible while the account was still there.
		      -- Without this, a closed account would contribute to net worth forever.
		      --
		      -- The missing-history leg is the #1626 Phase 2 ruling that absence of interval data is
		      -- "no reason to hide" rather than evidence: migration 000152's zero-length-interval
		      -- guard leaves some accounts with NO 'active' row at all, so keying the carry on a
		      -- recorded interval ALONE would silently make this whole fix a no-op for exactly those
		      -- accounts. It is narrow by construction: an account the user really disconnected has
		      -- an 'active' row followed by a terminal one, so the last leg governs it.
		      --
		      -- ⚠ IT ALSO DEMANDS accounts.status = 'active', AND THAT IS THE CORRECTION OF A LIVE
		      -- WRONG NUMBER (#1626 Phase 2, X1). Shaped only as NOT EXISTS(active interval) it read
		      -- as unconditional permission to carry, and TWO real shapes satisfy that forever:
		      -- migration 000152's removed branch DELIBERATELY writes a terminal-only history when
		      -- removed_at is NULL or <= created_at, and an orphan (account_id IS NULL, migration
		      -- 000150's ON DELETE SET NULL) has no history to find at all — so a closed account's
		      -- last retained balance was carried onto every later observed day, permanently. The
		      -- orphan case could not even be corrected downstream, because live_all inner-joins
		      -- accounts and supersession therefore never sees the carried row.
		      --
		      -- ⚠ THIS IS NOT accountWithNoActiveIntervalLeg, and it must not be made to match it.
		      -- That leg governs the transactions table — a dated thing that HAPPENED, retained
		      -- forever (#1569 EVENT universe), where no status may hide it. This governs an
		      -- INFERENCE that a balance persisted on a day nobody reported one, which is only
		      -- defensible while somebody is still confirming the account exists. Nothing retained
		      -- becomes unqueryable: the account's OWN snapshot days are preserved by the first leg
		      -- above, and its as-of balances are untouched.
		      AND (
		          o.snapshot_date = c.snapshot_date
		          OR EXISTS (
		              SELECT 1 FROM accounts a_carry
		              WHERE a_carry.id = o.account_id
		                AND a_carry.status = 'active'
		                AND NOT EXISTS (
		                    SELECT 1 FROM account_status_history ash_any
		                    WHERE ash_any.account_id = a_carry.id
		                      AND ash_any.status = 'active'
		                )
		          )
		          OR ` + accountLiveOnDateSQL("o.account_id", "c.snapshot_date") + `
		      )
		),
		-- live_all is built from the FILLED series, not the observed one, and that is what makes the
		-- succession collapse below correct after this change. Before the carry-forward a
		-- predecessor appeared only on days it actually reported, so an overlap day rarely held both
		-- chain members; now both are present on every day after their own first snapshot, which is
		-- exactly the input accountChainSupersessionCTE exists to collapse. Feeding it the observed
		-- rows instead would double-count every carried re-link.
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

// aggregatedHistoryTypePredicate is what GetAggregatedHistory renders into
// aggregatedHistorySQLBase's single hole when the caller filters by account type.
//
// ⚠ IT TESTS THE RECOVERED TYPE, NOT THE RAW COLUMN, and that is the correction of a live
// inconsistency (final whole-branch review, finding F5). Written as `AND a.type = ANY($4)` it
// disagreed with the projection in the same CTE, which is the COALESCE: an ORPHANED snapshot
// (accounts row gone, account_id SET NULL) has a NULL a.type, so the very same credit card
// appeared in ByType["credit"] for an unfiltered request and VANISHED for
// accountTypes=['credit']. One account, two answers, decided by a filter whose job is to SELECT
// rows rather than to redefine what they are.
//
// It is a named constant rather than an inline literal so the structural pin test can assert it:
// it lives OUTSIDE the template, so TestAggregatedHistorySQLBase_PinsItsLoadBearingFragments's
// scan of the template cannot see it. The household statement needs no equivalent — its filter is
// applied outer, to `k.account_type`, which IS the projection.
const aggregatedHistoryTypePredicate = "AND COALESCE(a.type, ua.identity_type) = ANY($4)"

// GetAggregatedHistory retrieves aggregated balance history for dashboard charts.
// The query LEFT JOINs accounts unconditionally so that per-type breakdowns are always
// available for net-worth computation. Orphaned snapshots (deleted account rows) are
// included in TotalBalance but omitted from ByType.
func (r *balanceSnapshotRepository) GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	// Canary counter: every call applies the re-link chain CTE dedup (Issue #1109).
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()

	// Base query with parameterized account-type filter in CTE.
	// If accountTypes is provided, aggregatedHistoryTypePredicate is rendered into the hole.
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
		query = fmt.Sprintf(queryBase, aggregatedHistoryTypePredicate)
		args = []interface{}{userID, startDate, endDate, accountTypes}
	} else {
		query = fmt.Sprintf(queryBase, "")
		args = []interface{}{userID, startDate, endDate}
	}

	return r.scanAggregatedHistory(ctx, query, args...)
}

// scanAggregatedHistory folds a (day, account_type, balance) result set into one
// domain.BalanceHistoryPoint per day.
//
// ONE implementation for BOTH scopes, deliberately. The two statements above it are the personal
// and household halves of the same read and emit the SAME three columns in the same order, grouped
// and ordered by day; a hand-copied fold in each method is a second place for the two scopes to
// drift, and the live bug this branch fixed was precisely the two halves having drifted apart.
// (The SQL itself stays as two statements — see the household statement's own header for the
// differences that are deliberate, notably grant clipping at instant vs day resolution.)
//
// The rows MUST arrive grouped by day, which both statements guarantee with GROUP BY/ORDER BY on
// snapshot_date: this walks the result once and starts a new point whenever the date changes, so an
// interleaved result set would emit the same day twice.
func (r *balanceSnapshotRepository) scanAggregatedHistory(ctx context.Context, query string, args ...interface{}) ([]domain.BalanceHistoryPoint, error) {
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
		} else {
			// #1626 Phase 3 item 5: an untyped balance is RECORDED rather than discarded, so the
			// net-worth reconstruction (dashboard_service.go GetNetWorthHistory), which is driven
			// entirely by ByType, can no longer read an orphan-only day as exactly zero. See
			// domain.BalanceHistoryPoint.UntypedBalance for why this is a field and not a
			// subtraction the consumer performs.
			current.UntypedBalance += bal
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
// Two maskless accounts at one institution were FORCED to share a user_accounts row (migration
// 000100's `UNIQUE (user_id, institution_id_blind_idx, mask_blind_idx) NULLS NOT DISTINCT`, plus
// FindByInstitutionAndMask's `mask_blind_idx IS NULL` branch), so a brokerage and an IRA at one
// broker were one chain key with two live accounts, and the chain rule DELETED one of them from
// every total. Measured, not reasoned: $100 + $400 reported as $400
// (TestMasklessPair_PersonalBalanceReads_CountBothAccounts).
//
// ⚠ #1569 round 11 task 4 narrowed FindByInstitutionAndMask's match key to also include
// identity_type/identity_subtype, so a brokerage and an IRA (same type, different SUBTYPE) no
// longer collide onto one chain — that SPECIFIC example above is now historical. The mechanism
// this fix guards is still live for the residual case the narrowing does NOT close: two accounts
// that agree on institution, mask, type, AND subtype still collide onto one chain — this is NOT
// limited to a NULL mask (an earlier version of this note said "both NULL"; that was wrong the
// same way task 4's household share fan-out was initially wrong about masklessness — the match
// key's mask comparison is a plain equality for non-NULL masks too, so two accounts sharing the
// SAME non-NULL mask collide identically) — so snapshotIdentityKey stays load-bearing for either
// shape.
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
		-- A STABLE ORDER, restored deliberately. The pre-#1569-T10.2 form was a single DISTINCT ON,
		-- whose ORDER BY also ordered the RESULT; splitting the statement in two dropped that, and an
		-- unordered result is user-visible here — GetBalanceSummary appends to
		-- AccountTypeBalance.Accounts in row order, so the account list under each type could have
		-- reshuffled between two identical requests.
		ORDER BY COALESCE(l.account_id::text, l.user_account_id::text, ''), l.id
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
// the freshest-synced copy. Issue #1369 — Phase 5.
//
// ⚠ IT RETURNS ONE ROW PER PHYSICAL ACCOUNT, NOT ONE PER user_account. This sentence used to say
// the DISTINCT ON "also collapses that chosen user_account_id to its single latest snapshot", and
// #1569 T10.2 changed exactly that (see snapshotIdentityKey) because collapsing to the
// user_account deletes one of two maskless siblings. It is corrected rather than deleted because
// callers were written against the old wording: dashboard_service.go had two loops that classified
// a snapshot by looking its CHAIN key up in an account map, which is only sound when a chain
// yields one row, and the household net-worth baseline was still doing it a round later
// (#1569 T10-FIX item 3 — a credit card counted as a depository asset).
func (r *balanceSnapshotRepository) GetSnapshotsAsOfForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()

	// #1569 T10.2: the inner grain is the PHYSICAL ACCOUNT, not the user_account.
	//
	// ⚠ THE TWO DEDUPS HERE ARE ABOUT DIFFERENT THINGS and only one of them belongs at the
	// user_account grain. household_accounts (above) resolves a JOINT ACCOUNT — the same physical
	// account linked and shared by two members — and picks one member's copy; that stays exactly as
	// it was, and $60 shared by two people is still $60. This DISTINCT ON was resolving something
	// else entirely: several snapshots of ONE shared unit down to the latest. When that unit holds
	// TWO accounts because they are maskless siblings, it silently kept one and dropped the other.
	//
	// ⚠ NO SCHEMA CHANGE IS NEEDED FOR THIS, and the plan's premise that one was is wrong —
	// measured rather than argued (TestMasklessPair_HouseholdBalanceReads_CountBothAccounts).
	// uq_household_account_shares_active constrains SHARE ROWS to one per (household,
	// user_account); it says nothing about how many accounts a share row COVERS. Both maskless
	// accounts hang off the shared user_account and both their snapshots already carry its id, so
	// they were always inside the household's grant — the query dropped one after the join, not
	// before it. Re-fanning the grants would have widened nothing and fixed nothing.
	query := "WITH RECURSIVE " + householdAccountsCTEForBalancesAsOf() + `,
		latest AS (
		    SELECT DISTINCT ON (` + snapshotIdentityKey + `)
		           bs.id, bs.account_id, bs.user_account_id, bs.user_id, bs.current_balance, bs.available_balance,
		           bs.currency_code, bs.snapshot_at, bs.source, bs.created_at
		    FROM balance_snapshots bs
		    JOIN household_accounts ha ON ha.user_account_id = bs.user_account_id
		    WHERE bs.snapshot_at <= $4
		    ORDER BY ` + snapshotIdentityKey + `, bs.snapshot_at DESC, bs.id DESC
		),
		live_all AS (
		    SELECT DATE '0001-01-01' AS snapshot_date, a.id AS account_id, a.user_id AS owner_id,
		           a.user_account_id, a.linked_item_id
		    FROM accounts a
		    JOIN latest l ON l.account_id = a.id
		),` + accountChainSupersessionCTESQL("TRUE") + `
		SELECT l.id, l.account_id, l.user_account_id, l.user_id, l.current_balance, l.available_balance,
		       l.currency_code, l.snapshot_at, l.source, l.created_at
		FROM latest l
		WHERE NOT EXISTS (
		    SELECT 1 FROM superseded x WHERE x.account_id = l.account_id
		)
		-- Stable order, for the same reason as the personal read: the household balance loop appends
		-- to its per-type account list in row order.
		ORDER BY COALESCE(l.account_id::text, l.user_account_id::text, ''), l.id
	`

	args := householdScopeArgs(requestingUserID, scope)
	args = append(args, asOf)
	return r.scanSnapshots(ctx, query, args...)
}

// aggregatedHistoryHouseholdSQLBase is GetAggregatedHistoryForHousehold's statement, with TWO %s
// holes: the household-grants CTE, and the optional account-type predicate.
//
// It is a package-level var for exactly the reason aggregatedHistorySQLBase is one — the structural
// pin test (TestAggregatedHistoryHouseholdSQLBase_PinsItsLoadBearingFragments) has to read the SAME
// string production renders, because the pgxmock tests build their ExpectQuery from the template
// itself and therefore move production and expectation together. A hand-copied duplicate of a
// 200-line statement would be a maintenance trap that told us nothing about correctness.
//
// ── The three-stage collapse, and where the carry-forward sits in it ─────────────────────────────
//
// observed -> per_account_per_day (the FILLED series) -> fingerprint_winner -> kept -> superseded.
//
// #1626 E2 introduced the fill. Before it, `per_account_per_day` WAS the observed rows, so a day's
// total was the sum of only the accounts that happened to receive a snapshot that day — and
// snapshots are written per-ITEM on webhook sync, so a day on which one member's item synced
// reported only that member's accounts and the household total collapsed toward zero. Every
// per-account balance was provider truth; the day-level AGGREGATE was not, and that distinction is
// the whole defect. The personal read (aggregatedHistorySQLBase) was fixed first; this is the
// household half of the same fix, and carrying forward REPAIRS EXISTING history, which no amount of
// future snapshotting can.
var aggregatedHistoryHouseholdSQLBase = `
		WITH RECURSIVE %s,
		observed AS (
		    -- #1569 T7, stage 1 of 3. ONE row per (day, user_account_id, ACCOUNT), applying the
		    -- grant INTERVAL clip that makes a departed member's shared period survive their
		    -- departure without leaking a single day past it.
		    --
		    -- ⚠ RENAMED FROM per_account_per_day BY #1626 E2, and the name was not freed for
		    -- decoration: per_account_per_day now means the FILLED series below, matching what the
		    -- personal statement calls the same thing. This CTE is the OBSERVED rows — what the
		    -- provider actually reported.
		    --
		    -- ⚠ UNBOUNDED BELOW ON PURPOSE. The caller's start date is applied to the FILLED series
		    -- instead. A day at the START of the window has to be able to inherit an account's last
		    -- balance from BEFORE the window, or a "this month" chart under-counts every account
		    -- whose most recent sync happened last month. Personal scope does this for the same
		    -- reason; its bound likewise lives on the filled series.
		    --
		    -- The user_account_id term is not redundant beside the account: household_accounts emits
		    -- one row per grant INTERVAL, a user_account can match several on the same day (closed
		    -- intervals are unconstrained by uq_household_account_shares_active and may overlap), and
		    -- a re-fingerprinted account changes fingerprint between intervals — so without this
		    -- collapse ONE account lands in two buckets and its balance is summed TWICE. Pinned by
		    -- TestHouseholdShareRange_OverlappingRefingerprintedIntervalsDoNotDoubleCount.
		    --
		    -- The interval in force wins (share_started_at DESC, share_row_id), matching
		    -- householdAccountsCTEForBalancesAsOf's rule, so the fingerprint carried onward is the one
		    -- that was actually recorded on that day.
		    SELECT DISTINCT ON ((bs.snapshot_at AT TIME ZONE 'UTC')::date, ha.user_account_id, bs.account_id)
		        (bs.snapshot_at AT TIME ZONE 'UTC')::date AS snapshot_date,
		        ha.user_account_id,
		        ha.fingerprint,
		        bs.account_id,
		        -- carry_key IS THE LOCF PARTITION, and it is a rendered text key rather than the two
		        -- columns themselves because bs.account_id is NULLABLE (ON DELETE SET NULL, migration
		        -- 000150): an equi-join on a NULL account_id matches nothing, so an orphaned snapshot
		        -- would lose even its OWN observed row on the way back out of the grid. The grain is
		        -- identical to the DISTINCT ON above, which is what keeps one account out of two
		        -- carry partitions.
		        ha.user_account_id::text || ':' || COALESCE(bs.account_id::text, '') AS carry_key,
		        bs.snapshot_at,
		        bs.id AS snapshot_id,
		        bs.current_balance,
		        -- ⚠ THE COALESCE IS THE HOUSEHOLD HALF OF #1626 PHASE 3 ITEM 5, and its absence was
		        -- a LIVE WRONG NUMBER (final whole-branch review, finding F1). An ORPHANED snapshot
		        -- (accounts row hard-deleted, account_id SET NULL by migration 000150) still knows
		        -- its LOGICAL account, and user_accounts.identity_type is that account's recorded
		        -- type — frozen once set, enforced by trg_user_accounts_identity_immutable.
		        --
		        -- The personal statement recovered it; this one read 'a.type' alone, so an orphan
		        -- emitted '' from the outer COALESCE, the Go loop routed it to UntypedBalance, and
		        -- GetNetWorthHistory adds a POSITIVE UntypedBalance to totalAssets. Plaid reports a
		        -- credit card's current_balance as a POSITIVE DEBT — so a household's orphaned
		        -- credit card ADDED its balance to assets instead of subtracting it, overstating
		        -- household net worth by TWICE the debt. Falling back to an untyped bucket is not a
		        -- safe default here: domain.AccountTypeCategory defaults to "assets", which is
		        -- exactly the sign guess this recovery exists to avoid.
		        --
		        -- The outer type predicate (AND k.account_type = ANY($6)) reads this projection,
		        -- so the filter and the breakdown agree by construction — the disagreement finding
		        -- F5 records in personal scope cannot arise here.
		        COALESCE(a.type, ua.identity_type) AS account_type
		    FROM balance_snapshots bs
		    JOIN household_accounts ha ON ha.user_account_id = bs.user_account_id
		    -- ⚠ THIS JOIN IS ON THE SNAPSHOT'S OWN ACCOUNT (a.id = bs.account_id), which #1569 T10.2
		    -- changed from a.user_account_id = bs.user_account_id. The old form matched EVERY member
		    -- of a chain, so a.type had to be chosen by an explicit preference — an ORDER BY term
		    -- reading (a.status = 'active') DESC NULLS LAST, a.id — and that preference had already
		    -- been wrong in both directions once (as a filtered join it dropped an expired-only
		    -- chain's history; with no preference at all it attributed a balance to a replaced
		    -- member's type). A snapshot knows which account it came from. Reading the type from that
		    -- account is what personal scope always did, needs no preference, and makes both pinned
		    -- properties hold by construction rather than by tie-break:
		    -- TestGetAggregatedHistoryForHousehold_RelinkChain_UsesActiveAccountType (the snapshot is
		    -- the active member's, so its type is the active member's) and
		    -- TestGetAggregatedHistoryForHousehold_ExpiredOnlyChain_KeepsTypeFilteredHistory (an
		    -- expired account still has a row, so a.type is not NULL and the type filter still
		    -- matches). LEFT, because bs.account_id is nullable (ON DELETE SET NULL) and an orphaned
		    -- snapshot must still reach the total, exactly as in personal scope — where, failing the
		    -- chain-row recovery below, it lands in the '' type bucket.
		    LEFT JOIN accounts a ON a.id = bs.account_id
		    -- The chain-row recovery, in the SAME words the personal statement uses: LEFT, and on
		    -- the snapshot's OWN chain pointer. It exists only to supply a type the accounts row can
		    -- no longer give, and user_accounts(id) is the PRIMARY KEY (migration 000033), so it
		    -- cannot multiply the row. It is a SECOND join to user_accounts — household_accounts
		    -- already joined that table to build its grants — because the grants CTE is keyed on the
		    -- GRANT, emitting one row per interval, whereas this needs the one row the snapshot
		    -- itself points at. Reading identity_type through ha would also work and was rejected
		    -- for keeping the two statements' recovery textually identical, so one structural
		    -- fragment pins both.
		    LEFT JOIN user_accounts ua ON ua.id = bs.user_account_id
		    -- The caller's window's UPPER end only, at date resolution because that is what it is: the
		    -- last calendar day the chart covers.
		    WHERE (bs.snapshot_at AT TIME ZONE 'UTC')::date <= $5::date
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
		      --
		      -- ⚠ IT IS ALSO WHAT MAKES THE CARRY SAFE AT THE OPENING END. A pre-grant instant never
		      -- becomes an observed row, so there is nothing for the fill to carry INTO the
		      -- share-start day — the household sees an account only from its first IN-GRANT
		      -- snapshot. Pinned by
		      -- TestHouseholdAggregatedHistoryCarry_ShareStartDay_DoesNotCarryAPreGrantInstant.
		      AND bs.snapshot_at >= ha.share_started_at
		      AND (ha.share_ended_at IS NULL OR bs.snapshot_at < ha.share_ended_at)
		    ORDER BY (bs.snapshot_at AT TIME ZONE 'UTC')::date, ha.user_account_id, bs.account_id,
		             ha.share_started_at DESC, ha.share_row_id,
		             bs.snapshot_at DESC, bs.id DESC
		),
		-- ── the grid the carry-forward fills ─────────────────────────────────────────────────
		--
		-- DAYS ARE NOT INVENTED, only VALUES. A chart point exists for a day only if some shared
		-- account actually reported on it; a day nobody synced at all is still absent from the series
		-- and the frontend still bridges it. That is deliberate and narrow: generating a full calendar
		-- grid would also generate rows for days in the FUTURE (endDate is unvalidated against today),
		-- turning a frontend-fabricated flat line into a backend-fabricated one. Same ruling, same
		-- wording, as the personal statement's observed_days. Pinned by
		-- TestHouseholdAggregatedHistoryCarry_IncompleteCohort_CarriesTheOtherMembersLastKnownBalance,
		-- which asserts the absent day as well as the carried one.
		observed_days AS (
		    SELECT DISTINCT snapshot_date FROM observed
		),
		observed_keys AS (
		    SELECT DISTINCT carry_key, user_account_id FROM observed
		),
		grid AS (
		    SELECT d.snapshot_date, k.carry_key, k.user_account_id
		    FROM observed_days d
		    CROSS JOIN observed_keys k
		),
		grant_on_day AS (
		    -- ⚠ THE PARTIAL-GRANT-DAY DECISION. This CTE is the one thing the personal carry-forward
		    -- has no analogue for, and it is a decision, not a mechanism.
		    --
		    -- Household grants are half-open intervals clipped for BALANCES at INSTANT resolution
		    -- (docs/decisions/2026-08-26-household-grants-cte-performance.md, ADR Decision 7), and the
		    -- observed clip above does exactly that. A CARRIED row has no instant to clip — it is this
		    -- query's inference about a whole calendar day nobody reported. So the rule has to be
		    -- stated at day resolution, and the ruling is DAY INTERSECTION: a carried value is
		    -- admitted on a day the grant covers even PARTIALLY, and refused on a day the grant does
		    -- not touch at all.
		    --
		    -- WHY INTERSECTION AND NOT FULL COVERAGE. Over the covered part of a revocation day the
		    -- inference is about instants the household was genuinely entitled to — the same
		    -- entitlement the instant clip expresses for real observations. Demanding full coverage
		    -- would drop the departing account's balance on the revocation day while other members
		    -- still report, which is precisely the collapse-toward-zero defect this fill exists to
		    -- remove, reintroduced on the most visible day in a household's life. And it leaks
		    -- nothing: the day AFTER the revocation has no row here, so the carry stops there.
		    -- Asserted in BOTH directions by
		    -- TestHouseholdAggregatedHistoryCarry_DepartedMember_CarriesThroughThePartialRevocationDayOnly.
		    --
		    -- IT ALSO RE-RESOLVES THE FINGERPRINT for the carried day rather than letting the source
		    -- day's travel forward. A re-fingerprinted account changes fingerprint between grant
		    -- intervals and the joint-account collapse below keys on the fingerprint, so a stale one
		    -- carried across a re-fingerprint would stop colliding with the other member's copy and
		    -- the same physical account would be summed twice.
		    --
		    -- The bounds are written the way accountLiveOnDateClause writes its own day-overlap test,
		    -- and for the same reason: "date + INTERVAL '1 day'" is a timestamp WITHOUT time zone,
		    -- which Postgres would otherwise resolve against the SESSION TimeZone, and the lower bound
		    -- needs the explicit ::timestamp step because a bare "date AT TIME ZONE 'UTC'" casts the
		    -- date to timestamptz FIRST (in the session zone) and then reads it out as UTC wall time.
		    SELECT DISTINCT ON (d.snapshot_date, ha.user_account_id)
		        d.snapshot_date,
		        ha.user_account_id,
		        ha.fingerprint
		    FROM observed_days d
		    JOIN household_accounts ha
		      ON ha.share_started_at < (d.snapshot_date::date + INTERVAL '1 day') AT TIME ZONE 'UTC'
		     AND (ha.share_ended_at IS NULL
		          OR ha.share_ended_at > (d.snapshot_date::date)::timestamp AT TIME ZONE 'UTC')
		    ORDER BY d.snapshot_date, ha.user_account_id, ha.share_started_at DESC, ha.share_row_id
		),
		carry_source AS (
		    -- LOCF: for each (day, carry_key), WHICH observed day supplies the balance — the latest at
		    -- or before this day. A running max() over the LEFT-JOINed grid is the idiom Postgres can
		    -- express without IGNORE NULLS, and it is O(rows log rows) rather than the O(days x rows)
		    -- a per-day LATERAL would cost on a multi-year window.
		    --
		    -- ⚠ THE JOIN EXCLUDES A NULL current_balance, and that is the same decision the personal
		    -- statement records at length. transactionSyncService accepts a snapshot with
		    -- current_balance NULL and available_balance present, and this aggregation sums only
		    -- current_balance — so such a row used to be dropped from the day's total silently, and on
		    -- a day whose ONLY row was one of those, SUM() returned SQL NULL and the scan failed
		    -- outright. COALESCE to available_balance was rejected: available balance is a DIFFERENT
		    -- QUANTITY, not a degraded version of the same one (on a credit card it is the remaining
		    -- credit line, so substituting it reports a large positive asset where a debt belongs).
		    -- "Last known CURRENT balance" is the honest answer and is what LOCF already computes. The
		    -- day itself still survives, because observed_days is built from observed, not from here.
		    SELECT g.snapshot_date,
		           g.carry_key,
		           max(o.snapshot_date) OVER (
		               PARTITION BY g.carry_key ORDER BY g.snapshot_date
		               ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
		           ) AS source_date
		    FROM grid g
		    LEFT JOIN observed o
		      ON o.carry_key = g.carry_key
		     AND o.snapshot_date = g.snapshot_date
		     AND o.current_balance IS NOT NULL
		),
		per_account_per_day AS (
		    -- ── THE FILLED SERIES, which is what the three-stage collapse below now consumes ──
		    --
		    -- Step 2b, the same decision personal scope made: history starts PER ACCOUNT, at that
		    -- account's earliest observed snapshot. A key with no observed row at or before the day
		    -- has a NULL source_date and this join drops it, so the total GROWS as accounts join
		    -- rather than starting on the first day every account happens to have data. The
		    -- alternative hides real history.
		    SELECT c.snapshot_date,
		           o.user_account_id,
		           -- An OWN row keeps the fingerprint recorded at its own instant; a CARRIED row takes
		           -- the one in force on the carried day (see grant_on_day). Mixing these up is how a
		           -- re-fingerprinted joint account would come to be counted twice.
		           CASE WHEN o.snapshot_date = c.snapshot_date THEN o.fingerprint
		                ELSE gd.fingerprint END AS fingerprint,
		           o.account_id,
		           o.snapshot_at,
		           o.snapshot_id,
		           o.current_balance,
		           o.account_type
		    FROM carry_source c
		    JOIN observed o
		      ON o.carry_key = c.carry_key
		     AND o.snapshot_date = c.source_date
		     AND o.current_balance IS NOT NULL
		    LEFT JOIN grant_on_day gd
		      ON gd.snapshot_date = c.snapshot_date
		     AND gd.user_account_id = o.user_account_id
		    WHERE c.snapshot_date >= $4::date
		      AND (
		          -- An account's OWN snapshot day keeps its value UNCONDITIONALLY. That row is
		          -- provider truth about a day the account really reported, already clipped to the
		          -- grant at instant resolution above, and re-asking account_status_history about it
		          -- would delete a closed account's retained history from the chart — the opposite of
		          -- what #1569 exists to do (see accountChainLiveAllCTE, which says so for the same
		          -- reason).
		          o.snapshot_date = c.snapshot_date
		          OR (
		              -- ⚠ A CARRIED ROW NEEDS BOTH GATES, and they answer different questions. gd asks
		              -- "was the household entitled to this account on this day at all?" — the
		              -- household-only half. The disjunction below asks "is anybody still confirming
		              -- this account exists?" — the half personal scope also carries. Dropping either
		              -- one produces a wrong number in a different direction: without gd a departed
		              -- member's balance is carried forever, without the liveness legs a CLOSED
		              -- account's is.
		              gd.user_account_id IS NOT NULL
		              AND (
		                  -- The missing-history leg is the #1626 Phase 2 ruling that absence of
		                  -- interval data is "no reason to hide" rather than evidence: migration
		                  -- 000152's zero-length-interval guard leaves some accounts with NO 'active'
		                  -- row at all, so keying the carry on a recorded interval ALONE would
		                  -- silently make this whole fix a no-op for exactly those accounts. It is
		                  -- narrow by construction: an account the user really disconnected has an
		                  -- 'active' row followed by a terminal one, so the last leg governs it.
		                  --
		                  -- ⚠ IT ALSO DEMANDS accounts.status = 'active', AND THAT TERM IS THE
		                  -- CORRECTION OF A LIVE WRONG NUMBER IN THE PERSONAL READ (#1626 Phase 2,
		                  -- X1), NOT REPEATED HERE. Shaped only as NOT EXISTS(active interval) the leg
		                  -- reads as unconditional permission to carry, and TWO real shapes satisfy
		                  -- that forever: migration 000152's removed branch DELIBERATELY writes a
		                  -- terminal-only history when removed_at is NULL or <= created_at, and an
		                  -- orphan (account_id IS NULL, migration 000150's ON DELETE SET NULL) has no
		                  -- history to find at all — so a closed account's last retained balance was
		                  -- carried onto every later observed day, permanently.
		                  --
		                  -- ⚠ THIS IS NOT accountWithNoActiveIntervalLeg, and it must not be made to
		                  -- match it. That leg governs the transactions table — a dated thing that
		                  -- HAPPENED, retained forever (#1569 EVENT universe), where no status may hide
		                  -- it. This governs an INFERENCE that a balance persisted on a day nobody
		                  -- reported one, which is only defensible while somebody is still confirming
		                  -- the account exists. Nothing retained becomes unqueryable: the account's OWN
		                  -- snapshot days are preserved by the first leg above, and its as-of balances
		                  -- are untouched. Pinned by
		                  -- TestHouseholdAggregatedHistoryCarry_RemovedAccount_IsNotCarriedPastItsRemoval.
		                  EXISTS (
		                      SELECT 1 FROM accounts a_carry
		                      WHERE a_carry.id = o.account_id
		                        AND a_carry.status = 'active'
		                        AND NOT EXISTS (
		                            SELECT 1 FROM account_status_history ash_any
		                            WHERE ash_any.account_id = a_carry.id
		                              AND ash_any.status = 'active'
		                        )
		                  )
		                  OR ` + accountLiveOnDateSQL("o.account_id", "c.snapshot_date") + `
		              )
		          )
		      )
		),
		fingerprint_winner AS (
		    -- Stage 2: resolve joint-account fingerprint collisions PER DAY — the same physical
		    -- account linked and shared by TWO members must be counted ONCE, and it is the
		    -- freshest-synced member's copy that wins, chosen independently each day so a member who
		    -- only started syncing mid-range does not erase the other's earlier history.
		    --
		    -- ⚠ IT PICKS A WINNING user_account, NOT A WINNING ROW. Picking a row is what the
		    -- pre-#1569-T10.2 version did, and with two maskless accounts behind one user_account it
		    -- discarded the other one. The winner's ENTIRE set of accounts survives stage 3.
		    --
		    -- ⚠ IT NOW RANKS CARRIED ROWS TOO, and snapshot_at is what makes that come out right: a
		    -- carried row carries its SOURCE instant, which is by construction older than any
		    -- observation made on the day itself, so a real observation always outranks an inference
		    -- about the same physical account. Pinned by
		    -- TestHouseholdAggregatedHistoryCarry_JointAccount_CarriedCopyDoesNotDoubleCount.
		    SELECT DISTINCT ON (snapshot_date, fingerprint)
		        snapshot_date, fingerprint, user_account_id
		    FROM per_account_per_day
		    ORDER BY snapshot_date, fingerprint, snapshot_at DESC, snapshot_id DESC, user_account_id
		),
		kept AS (
		    SELECT p.*
		    FROM per_account_per_day p
		    JOIN fingerprint_winner w
		      ON w.snapshot_date = p.snapshot_date
		     AND w.fingerprint = p.fingerprint
		     AND w.user_account_id = p.user_account_id
		),
		-- live_all is built from the FILLED series, not the observed one, and that is what makes the
		-- succession collapse below correct after the carry-forward. Before the fill a predecessor
		-- appeared only on days it actually reported, so an overlap day rarely held both chain
		-- members; now both are present on every day after their own first snapshot, which is exactly
		-- the input accountChainSupersessionCTE exists to collapse. Feeding it the observed rows
		-- instead would double-count every carried re-link. Same note as the personal statement's.
		live_all AS (
		    SELECT k.snapshot_date, a.id AS account_id, a.user_id AS owner_id,
		           a.user_account_id, a.linked_item_id
		    FROM kept k
		    JOIN accounts a ON a.id = k.account_id
		),` + accountChainSupersessionCTESQL("TRUE") + `
		SELECT k.snapshot_date,
		       COALESCE(k.account_type, '') AS account_type,
		       SUM(k.current_balance) AS type_balance
		FROM kept k
		-- Stage 3: a genuine re-link chain, whose two members are two ACCOUNTS behind one
		-- user_account and would otherwise both be summed on the overlap day. Same recorded-
		-- succession rule the personal reads apply; see accountChainSupersessionCTE.
		WHERE NOT EXISTS (
		    SELECT 1 FROM superseded x
		    WHERE x.snapshot_date = k.snapshot_date
		      AND x.account_id = k.account_id
		)
		-- ⚠ THE TYPE FILTER IS APPLIED HERE, TO THE PER-DAY WINNER, and it deliberately does NOT move
		-- into the observed CTE the way personal scope's does. Filtering the DISTINCT ON candidate set first
		-- would let a stale copy whose type happens to match beat the freshest copy for a day — pinned
		-- by
		-- TestBalanceSnapshotRepository_GetAggregatedHistoryForHousehold_JointAccountTypeFilterAppliesToWinner.
		--
		-- The consequence, stated rather than left to be discovered: observed_days is built BEFORE the
		-- type filter, so a day whose only snapshot belongs to a filtered-out type still exists as a
		-- chart day here (carrying the matching accounts' last known balances), whereas personal
		-- scope — whose type predicate is rendered inside its own observed CTE — drops that day. Both
		-- are defensible; they are not the same, and the difference is forced by the tie-break above.
		%s
		GROUP BY k.snapshot_date, k.account_type
		ORDER BY k.snapshot_date ASC, account_type ASC
	`

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
// ⚠ THE PARAGRAPH THAT WAS HERE DESCRIBED A JOIN THIS QUERY NO LONGER HAS, and it is replaced
// rather than trimmed because its reasoning is the reasoning that has to stop. It said
// account_type comes from "whichever accounts row the LEFT JOIN matches by user_account_id", with
// an ORDER BY preferring the ACTIVE chain member and an a.id tiebreak for the case where two
// active rows share a user_account_id — a case it called should-be-impossible on the strength of
// the re-link handler. It is not impossible: migration 000100 FORCES two maskless accounts at one
// institution onto one user_accounts row, so the "tiebreak" was routinely deciding which of two
// real accounts a balance was attributed to.
//
// #1569 T10.2 replaced it with `a.id = bs.account_id`. A snapshot knows which account it came
// from; reading the type from that account needs no preference and is what personal scope always
// did. current_balance likewise always comes from the selected balance_snapshots row.
//
// Pinned by TestGetAggregatedHistoryForHousehold_RelinkChain_ASnapshotKeepsItsOwnAccountsType,
// which is the one that actually separates the two joins — see its comment for why the test that
// preceded it did not (#1569 T10-FIX item 6).
func (r *balanceSnapshotRepository) GetAggregatedHistoryForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("dashboard_history").Inc()

	// #1569 T10.2: STAGE 1 IS NOW PER ACCOUNT, and stage 2 resolves the joint-account fingerprint by
	// choosing a WINNING user_account rather than a winning row.
	//
	// ⚠ THE TWO COLLAPSES MUST NOT BE THE SAME COLLAPSE. Keying either stage on the user_account
	// alone deletes one of two maskless accounts (migration 000100 forces them to share one
	// user_accounts row), and keying stage 2 on the ACCOUNT alone double-counts a joint account
	// that two members both linked. So: stage 2 picks ONE user_account per (day, fingerprint) — $60
	// shared by two people is still $60, unchanged — and then EVERY account behind that winner is
	// kept.
	//
	// ⚠ No schema change was needed; see GetSnapshotsAsOfForHousehold for the measurement that
	// retired the "the share grain must move" premise.
	queryBase := aggregatedHistoryHouseholdSQLBase

	args := householdScopeArgs(requestingUserID, scope)
	args = append(args, startDate, endDate)

	var query string
	if len(accountTypes) > 0 {
		args = append(args, accountTypes)
		query = fmt.Sprintf(queryBase, householdAccountsCTEForBalancesPerDay(), "AND k.account_type = ANY($6)")
	} else {
		query = fmt.Sprintf(queryBase, householdAccountsCTEForBalancesPerDay(), "")
	}

	// Same fold as personal scope, and shared with it on purpose — see scanAggregatedHistory.
	return r.scanAggregatedHistory(ctx, query, args...)
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
