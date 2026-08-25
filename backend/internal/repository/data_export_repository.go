package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// DataExportRepository reads the authenticated user's own data out of every user-scoped table, for
// the GDPR Article 20 export (#1587).
type DataExportRepository interface {
	// ExportUserData returns every exportable row this user owns, keyed by source table.
	ExportUserData(ctx context.Context, userID string) (*domain.DataExport, error)
}

// DataExportTable declares how ONE live table is exported.
//
// ⚠ COLUMNS ARE NOT LISTED HERE, DELIBERATELY. The row is read with `to_jsonb(t)`, so a column
// added by a later migration is exported without anyone remembering to widen a list. That is the
// opposite of the failure mode that produced #1581 (a hand-copied table list that went stale), and
// it is why the only column-level state below is a DENYLIST: forgetting to add a column exports it,
// which is the safe direction for portability, and the two guards
// (TestDataExportClassifiesEveryUserScopedTable, TestDataExportWithholdsEverySecretShapedColumn)
// close the unsafe direction.
type DataExportTable struct {
	// Table is the live table name and is also the section key in the export document.
	Table string

	// OwnershipPredicate is a SQL boolean over the alias `t`, with `$1` bound to the exporting
	// user's id. It MUST establish that the row belongs to THIS user. It must never reach another
	// user's rows through a household join — see the doc comment on domain.DataExport.
	OwnershipPredicate string

	// OrderBy is a SQL ORDER BY list over the alias `t`, so the export is byte-stable across runs.
	OrderBy string

	// ExcludedColumns are dropped from every row of this table, on top of
	// dataExportGlobalExcludedColumns. Each one needs a reason in the comment beside it: either it
	// is a secret, or it identifies a DIFFERENT data subject.
	ExcludedColumns []string

	// DecryptedColumns are stored encrypted at rest ("ENC$"+ciphertext) but their plaintext IS this
	// user's own data, so the export carries the plaintext. Exporting the ciphertext would be worse
	// than useless: unreadable to the user and still a copy of a secret-adjacent value.
	//
	// Listing a column that happens to hold plaintext is harmless — FieldEncryptor.DecryptString
	// passes through any value lacking the "ENC$" prefix (dual-read support).
	DecryptedColumns []string
}

// dataExportGlobalExcludedColumns are dropped from EVERY table, matched by exact column name.
//
// The rule, stated once so it can be applied to a new column without re-deriving it: a column is
// withheld when it is (a) a credential or a secret, (b) a derived index of a secret, or (c) an
// identifier issued by a third party for authentication rather than for the user's own reference.
var dataExportGlobalExcludedColumns = map[string]string{
	// (a) credentials.
	"access_token_encrypted": "Plaid access token — a live bearer credential for the user's bank connection",
	"token_hash":             "household invite token hash — a bearer credential for accepting an invite",

	// (c) third-party authentication identifiers.
	"clerk_user_id": "Clerk's internal authentication identifier; not data the user provided or can act on",
}

// dataExportGlobalExcludedSuffixes are dropped from every table by suffix match. Blind indexes are
// HMACs of an encrypted column under a key derived from the master encryption key; they are
// unreadable to the user and are (b) a derived index of a secret.
var dataExportGlobalExcludedSuffixes = map[string]string{
	"_blind_idx": "blind index — an HMAC of an encrypted column, unreadable and derived from a secret",
}

// DataExportTables is the export's table set.
//
// ⚠ Its completeness is not maintained by hand: TestDataExportClassifiesEveryUserScopedTable
// re-derives the user-scoped universe from the LIVE schema (the transitive ON DELETE CASCADE
// closure from `users`) plus the explicit `DELETE FROM`s inside DeleteUserCascade, and fails when a
// table in that universe appears neither here nor in DataExportWithheldTables. If a migration adds
// a user-scoped table, that guard is what tells you.
var DataExportTables = []DataExportTable{
	// --- Identity and account ------------------------------------------------------------------
	{
		Table:              "users",
		OwnershipPredicate: "t.id = $1",
		OrderBy:            "t.id",
		DecryptedColumns:   []string{"email", "paddle_customer_id", "paddle_subscription_id"},
	},
	{
		Table:              "user_identities",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
		DecryptedColumns:   []string{"email"},
	},
	{
		Table:              "privacy_settings",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.id",
	},
	{
		Table:              "user_dashboard_preferences",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.user_id",
	},
	{
		Table:              "user_tags",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},

	// --- Connections and accounts --------------------------------------------------------------
	{
		Table:              "linked_items",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
		DecryptedColumns:   []string{"item_id", "institution_id", "institution_name"},
	},
	{
		Table:              "user_accounts",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
		DecryptedColumns:   []string{"institution_id", "mask", "account_name_hint"},
	},
	{
		Table:              "accounts",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
		DecryptedColumns:   []string{"plaid_account_id", "name", "persistent_account_id"},
	},
	{
		Table:              "account_status_history",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "balance_snapshots",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.snapshot_at, t.id",
	},

	// --- Transactions --------------------------------------------------------------------------
	{
		Table:              "transactions",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.date, t.id",
	},
	{
		Table:              "transaction_annotation_history",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.changed_at, t.id",
	},
	{
		Table:              "merchant_name_overrides",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "custom_categories",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "categorization_rules",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "rule_execution_history",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.executed_at, t.id",
	},

	// --- Budgets -------------------------------------------------------------------------------
	{
		Table:              "budgets",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		// budget_categories has no user_id of its own — its only route to a user is its budget.
		// That is exactly the FK the erasure cascade follows, so scoping through it exports the
		// same rows the user's own deletion would destroy, and no others.
		Table:              "budget_categories",
		OwnershipPredicate: "t.budget_id IN (SELECT b.id FROM budgets b WHERE b.user_id = $1)\n-- tombstone-exempt(budgets b): a tombstoned parent is RETAINED history, not destroyed data (#1569), and the export is everything BoostedBudget still holds about this user. Filtering deleted_at here would hand the user a smaller archive than the one we keep, which is the opposite of what Article 15 asks. The rows stay invisible in the product; they are visible in the user's own export.",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "budget_period_events",
		OwnershipPredicate: "t.budget_id IN (SELECT b.id FROM budgets b WHERE b.user_id = $1)\n-- tombstone-exempt(budgets b): a tombstoned parent is RETAINED history, not destroyed data (#1569), and the export is everything BoostedBudget still holds about this user. Filtering deleted_at here would hand the user a smaller archive than the one we keep, which is the opposite of what Article 15 asks. The rows stay invisible in the product; they are visible in the user's own export.",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "rollover_history",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},

	// --- Savings, boosts, suggestions ----------------------------------------------------------
	{
		Table:              "savings_goals",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "savings_goal_contributions",
		OwnershipPredicate: "t.savings_goal_id IN (SELECT g.id FROM savings_goals g WHERE g.user_id = $1)\n-- tombstone-exempt(savings_goals g): a tombstoned parent is RETAINED history, not destroyed data (#1569), and the export is everything BoostedBudget still holds about this user. Filtering deleted_at here would hand the user a smaller archive than the one we keep, which is the opposite of what Article 15 asks. The rows stay invisible in the product; they are visible in the user's own export.",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "savings_goal_category_detach",
		OwnershipPredicate: "t.savings_goal_id IN (SELECT g.id FROM savings_goals g WHERE g.user_id = $1)\n-- tombstone-exempt(savings_goals g): a tombstoned parent is RETAINED history, not destroyed data (#1569), and the export is everything BoostedBudget still holds about this user. Filtering deleted_at here would hand the user a smaller archive than the one we keep, which is the opposite of what Article 15 asks. The rows stay invisible in the product; they are visible in the user's own export.",
		OrderBy:            "t.savings_goal_id",
	},
	{
		Table:              "boosts",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "boost_executions",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.started_at, t.id",
	},
	{
		Table:              "transfer_suggestions",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},

	// --- Investments ---------------------------------------------------------------------------
	{
		Table:              "holdings",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "holding_snapshots",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.as_of_date, t.id",
	},
	{
		Table:              "investment_transactions",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.date, t.id",
	},
	{
		Table:              "portfolio_snapshots",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.snapshot_date, t.id",
	},
	{
		Table:              "portfolio_snapshot_repairs",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.examined_at, t.id",
	},

	// --- Liabilities ---------------------------------------------------------------------------
	{
		Table:              "credit_liabilities",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "credit_liability_aprs",
		OwnershipPredicate: "t.credit_liability_id IN (SELECT c.id FROM credit_liabilities c WHERE c.user_id = $1)",
		OrderBy:            "t.id",
	},
	{
		Table:              "mortgage_liabilities",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "student_loan_liabilities",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "liability_statements",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},

	// --- Household -----------------------------------------------------------------------------
	//
	// ⚠ EVERY PREDICATE BELOW IS ROOTED AT THIS USER, NOT AT THE HOUSEHOLD. Scoping a household
	// child table by `household_id` would export the peers' rows too, which is precisely the
	// Article 20(4) violation this export must not commit.
	{
		Table: "households",
		// Membership, not ownership: a non-owner member is entitled to the household record they
		// belong to (its name and lifecycle), and only that record.
		OwnershipPredicate: "t.owner_user_id = $1 OR t.id IN (SELECT hm.household_id FROM household_members hm WHERE hm.user_id = $1)",
		OrderBy:            "t.created_at, t.id",
		// owner_user_id identifies a DIFFERENT data subject whenever this user is a member rather
		// than the owner.
		ExcludedColumns: []string{"owner_user_id"},
	},
	{
		Table:              "household_members",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "household_share_consents",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "household_account_shares",
		OwnershipPredicate: "t.user_account_id IN (SELECT ua.id FROM user_accounts ua WHERE ua.user_id = $1)",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "household_invites",
		OwnershipPredicate: "t.inviter_user_id = $1 OR t.accepted_user_id = $1",
		OrderBy:            "t.created_at, t.id",
		// Both columns identify a DIFFERENT data subject in one of the two directions this
		// predicate matches: accepted_user_id when this user sent the invite, inviter_user_id when
		// this user accepted it. invitee_email is retained — it is either this user's own address
		// or an address this user typed in themselves.
		ExcludedColumns:  []string{"inviter_user_id", "accepted_user_id"},
		DecryptedColumns: []string{"invitee_email"},
	},

	// --- Billing -------------------------------------------------------------------------------
	{
		Table:              "subscription_entitlements",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
		// payer_user_id is a different data subject on a household plan the user did not pay for.
		ExcludedColumns: []string{"payer_user_id"},
	},
	{
		Table:              "user_subscription_overrides",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "paddle_transactions",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
		DecryptedColumns:   []string{"transaction_id"},
	},
	{
		Table:              "referral_codes",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "referrals",
		OwnershipPredicate: "t.referrer_id = $1 OR t.referred_id = $1",
		OrderBy:            "t.created_at, t.id",
		// Both ends of a referral are user ids, and exactly one of them is somebody else. There is
		// no direction in which exporting the pair is only this user's data, so neither is
		// exported; the row still records that a referral happened, its status and its dates.
		ExcludedColumns: []string{"referrer_id", "referred_id"},
	},

	// --- Everything else -----------------------------------------------------------------------
	{
		Table:              "notifications",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "mcp_recipes",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "mcp_audit_log",
		OwnershipPredicate: "t.actor_user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
}

// DataExportWithheldTables classifies the user-scoped tables that are deliberately NOT exported.
//
// This map is the second half of TestDataExportClassifiesEveryUserScopedTable's coverage: the guard
// fails on a live user-scoped table that appears in neither DataExportTables nor here, so "we
// forgot" cannot masquerade as "we decided". The reason is the whole point of the entry — an entry
// without a defensible reason is a silent portability gap.
var DataExportWithheldTables = map[string]string{
	"user_email_prefix_indices": "Internal lookup shard for email-prefix search. Its only non-key " +
		"column is a blind index, which is unreadable to the user and derived from the master " +
		"encryption key; the email it indexes is exported in full under `users`.",
	"idempotency_keys": "Internal request de-duplication. The keys are opaque client-generated " +
		"tokens and response_body is a cached copy of an API response the user already received; " +
		"rows are short-lived by construction.",
	"dead_letter_jobs": "Internal background-job failure records. args/errors are job payloads and " +
		"Go error strings, which describe BoostedBudget's processing rather than the user's " +
		"finances; the financial rows a failed job would have written are exported from their own " +
		"tables.",
	"audit_logs": "Security audit trail. It is retained under a legal-obligation basis rather than " +
		"held on the user's behalf, it is redacted rather than deleted on erasure, and its " +
		"`changes` column can carry before/after values of rows that were subsequently corrected. " +
		"Requests for it go through privacy@boostedbudget.com so they can be answered individually.",
}

// dataExportRepository implements DataExportRepository.
type dataExportRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
	// now is injectable so a test can pin GeneratedAt.
	now func() time.Time
}

// NewDataExportRepository creates a DataExportRepository.
func NewDataExportRepository(db DB, enc *appcrypto.FieldEncryptor) DataExportRepository {
	return &dataExportRepository{db: db, enc: enc, now: func() time.Time { return time.Now().UTC() }}
}

// dataExportRowQuery builds the one statement used for every section.
//
// `to_jsonb(t)` is what keeps the export from drifting away from the schema: the row is serialised
// by Postgres with whatever columns it currently has, so a migration that adds a column adds it to
// the export, and a migration that drops one drops it, with no Go change and no list to forget.
// ⚠ THE CLAUSES ARE ON SEPARATE LINES DELIBERATELY. Several ownership predicates carry a trailing
// `-- tombstone-exempt(...)` comment (see below); spliced into a single-line statement, such a
// comment would swallow the rest of the query. TestDataExportQueriesMatchTheLiveSchema runs the
// output of THIS function against Postgres, so a regression to one line fails there rather than in
// production.
func DataExportRowQuery(tbl DataExportTable) string {
	return fmt.Sprintf(
		"SELECT to_jsonb(t)\nFROM %s t\nWHERE %s\nORDER BY %s",
		tbl.Table, tbl.OwnershipPredicate, tbl.OrderBy,
	)
}

// isGloballyExcluded reports whether a column is withheld from every table.
func isGloballyExcluded(column string) bool {
	if _, ok := dataExportGlobalExcludedColumns[column]; ok {
		return true
	}
	for suffix := range dataExportGlobalExcludedSuffixes {
		if strings.HasSuffix(column, suffix) {
			return true
		}
	}
	return false
}

// ExportUserData reads every exportable table for one user.
//
// One statement per table, each of them a single indexed read on the user's own id. There is no
// transaction: an export is a read-only snapshot for the user's own records, and holding a
// repeatable-read transaction open across ~48 statements would put a long-lived snapshot on the
// primary for no benefit the user can perceive. The cost is that a write landing mid-export can be
// present in one section and absent from another; GeneratedAt is therefore an approximation, and it
// is documented as one.
func (r *dataExportRepository) ExportUserData(ctx context.Context, userID string) (*domain.DataExport, error) {
	export := &domain.DataExport{
		ExportVersion:  domain.DataExportVersion,
		GeneratedAt:    r.now(),
		UserID:         userID,
		Sections:       make(map[string][]map[string]any, len(DataExportTables)),
		ExcludedTables: make(map[string]string, len(DataExportWithheldTables)),
	}
	for tbl, reason := range DataExportWithheldTables {
		export.ExcludedTables[tbl] = reason
	}

	for _, tbl := range DataExportTables {
		section, err := r.exportTable(ctx, tbl, userID)
		if err != nil {
			return nil, err
		}
		export.Sections[tbl.Table] = section
		export.RowCount += len(section)
	}

	return export, nil
}

func (r *dataExportRepository) exportTable(ctx context.Context, tbl DataExportTable, userID string) ([]map[string]any, error) {
	rows, err := r.db.Query(ctx, DataExportRowQuery(tbl), userID)
	if err != nil {
		return nil, fmt.Errorf("ExportUserData: query %s: %w", tbl.Table, err)
	}
	defer rows.Close()

	excluded := make(map[string]struct{}, len(tbl.ExcludedColumns))
	for _, col := range tbl.ExcludedColumns {
		excluded[col] = struct{}{}
	}

	// Empty rather than nil: a section that serialises as `[]` says "we hold nothing here", where
	// `null` reads as "this was not attempted".
	out := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("ExportUserData: scan %s: %w", tbl.Table, err)
		}

		// UseNumber, not float64. Every monetary column in this schema is NUMERIC, and decoding
		// through float64 would silently re-round the user's money on the way out of the building.
		// json.Number keeps the digits Postgres emitted and re-marshals them verbatim.
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		row := map[string]any{}
		if err := dec.Decode(&row); err != nil {
			return nil, fmt.Errorf("ExportUserData: decode %s row: %w", tbl.Table, err)
		}

		for col := range row {
			if isGloballyExcluded(col) {
				delete(row, col)
				continue
			}
			if _, drop := excluded[col]; drop {
				delete(row, col)
			}
		}

		for _, col := range tbl.DecryptedColumns {
			if err := r.decryptColumn(row, tbl.Table, col); err != nil {
				return nil, err
			}
		}

		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ExportUserData: iterate %s: %w", tbl.Table, err)
	}

	return out, nil
}

// decryptColumn replaces an at-rest ciphertext with its plaintext, in place.
//
// A column that is absent (dropped by a migration, or excluded above) or NULL is left alone: the
// export must not invent a value, and a decrypt list that has outlived its column is a stale list,
// not a failure of this request. A column present but undecryptable IS a failure — returning
// "ENC$..." to the user would be a silently corrupt export.
func (r *dataExportRepository) decryptColumn(row map[string]any, table, column string) error {
	value, present := row[column]
	if !present || value == nil {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	plain, err := r.enc.DecryptString(text)
	if err != nil {
		return fmt.Errorf("ExportUserData: decrypt %s.%s: %w", table, column, err)
	}
	row[column] = plain
	return nil
}

// IsDataExportColumnGloballyExcluded reports whether a column is withheld from every exported
// table by the global rule. Exported so the guard can ask the production rule the question rather
// than re-implementing it — a guard with its own copy of the rule stops guarding the moment the two
// drift.
func IsDataExportColumnGloballyExcluded(column string) bool { return isGloballyExcluded(column) }

// DataExportTableNames returns the exported table names, sorted. Used by the guards and by tests
// that need a stable ordering.
func DataExportTableNames() []string {
	names := make([]string, 0, len(DataExportTables))
	for _, tbl := range DataExportTables {
		names = append(names, tbl.Table)
	}
	sort.Strings(names)
	return names
}
