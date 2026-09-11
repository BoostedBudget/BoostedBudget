package repository

import (
	"bytes"
	"context"
	"encoding/base64"
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
}

const (
	// dataExportCiphertextPrefix mirrors crypto's unexported envelope marker. appcrypto.IsEncrypted
	// is the authority on whether a value carries it; this constant only exists to strip it.
	dataExportCiphertextPrefix = "ENC$"

	// dataExportMinCiphertextBytes is the smallest byte count a real envelope can decode to: a
	// 12-byte AES-GCM nonce plus the 16-byte authentication tag, with an empty plaintext.
	dataExportMinCiphertextBytes = 12 + 16
)

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
	},
	{
		Table:              "user_identities",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
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
	},
	{
		Table:              "user_accounts",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
	},
	{
		Table:              "accounts",
		OwnershipPredicate: "t.user_id = $1",
		OrderBy:            "t.created_at, t.id",
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

	// --- Budgets --------------------------------------------------------------------------------
	{
		// Issue #1630 Task B7: the budget-hierarchy audit ledger, same shape as
		// portfolio_snapshot_repairs directly above — one row per budget the audit examined,
		// user-scoped, exportable under Article 20 exactly like every other examined-row ledger.
		Table:              "budget_hierarchy_audits",
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
		ExcludedColumns: []string{"inviter_user_id", "accepted_user_id"},
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
	"linked_item_repair_nonces": "Internal single-use authorization artifact (#1620). Each row is " +
		"a sha256 digest of a nonce the user's own browser held for the minutes of one Plaid Link " +
		"reconnection, plus two foreign keys and three timestamps. The digest is one-way and the " +
		"raw value was never stored, so there is nothing in the row the user could read, verify " +
		"or re-use; and the fact it records — that they reconnected a bank — is already visible " +
		"to them as the connection's restored status under `linked_items`. Rows are dead within " +
		"the Plaid link token's own expiry (~30 minutes) by construction.",
	"idempotency_keys": "Internal request de-duplication. The keys are opaque client-generated " +
		"tokens and response_body is a cached copy of an API response the user already received; " +
		"rows are short-lived by construction.",
	"dead_letter_jobs": "Internal background-job failure records. args/errors are job payloads and " +
		"Go error strings, which describe BoostedBudget's processing rather than the user's " +
		"finances; the financial rows a failed job would have written are exported from their own " +
		"tables.",
	"holding_observations": "Internal capture marker (#1569 T9-FIX). One row per (account, UTC " +
		"day) recording that a holdings capture COMPLETED — it carries no value, no position and " +
		"nothing the user reported or entered. What the capture FOUND is exported in full under " +
		"`holding_snapshots`; this table exists only so that an account observed holding nothing " +
		"is distinguishable from one never observed, which is a fact about our sync schedule " +
		"rather than about the user's finances.",
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

	tally := &envelopeTally{}
	for _, tbl := range DataExportTables {
		section, err := r.exportTable(ctx, tbl, userID, tally)
		if err != nil {
			return nil, err
		}
		export.Sections[tbl.Table] = section
		export.RowCount += len(section)
	}

	if err := tally.exportKeyHealthError(); err != nil {
		return nil, err
	}

	return export, nil
}

func (r *dataExportRepository) exportTable(
	ctx context.Context, tbl DataExportTable, userID string, tally *envelopeTally,
) ([]map[string]any, error) {
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

		r.decryptEveryEnvelope(row, tbl.Table, tally)

		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ExportUserData: iterate %s: %w", tbl.Table, err)
	}

	return out, nil
}

// decryptEveryEnvelope replaces every at-rest ciphertext in the row with its plaintext, in place.
//
// ⚠ THIS IS DERIVED, NOT LISTED, AND THAT IS THE WHOLE POINT. The first draft of this file carried
// a per-table `DecryptedColumns` list, and the list was wrong the day it was written: fifteen
// encrypted columns were missing from it — accounts.official_name and .mask,
// transactions.external_transaction_id and .external_metadata, every mortgage and student-loan
// address and account field, paddle_transactions.payload/subscription_id/customer_id,
// user_identities.provider_user_id, notifications.action_url — so the export handed the data
// subject unusable "ENC$…" strings where their own data belonged. Nothing noticed, because the
// integration test decrypted-and-checked exactly one column and the secret-shaped guard's column
// pattern (token|secret|password|clerk|encrypt|cipher|blind_idx|hash) matches none of those names.
//
// A hand-maintained encrypted-column list is the same failure mode as the hand-maintained table
// list that produced #1581. So the question asked here is not "is this column on the list" but "is
// this VALUE wearing the envelope this application puts on ciphertext" — which no migration, no new
// repository and no forgotten review can get wrong.
//
// ⚠ A VALUE THAT WILL NOT AUTHENTICATE IS NOT OURS. It is exported verbatim rather than failing the
// request; only a document in which NOTHING decrypted is treated as a broken key. See envelopeTally.
//
// ⚠ THE EXCLUSIONS RUN FIRST, IN THE CALLER, AND THE ORDER IS LOAD-BEARING. access_token_encrypted
// is a live Plaid bearer credential that is also ENC$-enveloped; it is deleted from the row before
// this function ever sees it, so "decrypt everything encrypted" never means "hand out the token".
// A future withheld ciphertext column must be withheld the same way — by exclusion, not by being
// left off a decrypt list, because there is no longer a decrypt list to be left off.
func (r *dataExportRepository) decryptEveryEnvelope(row map[string]any, table string, tally *envelopeTally) {
	for column, value := range row {
		text, ok := value.(string)
		if !ok || !isCiphertextEnvelope(text) {
			continue
		}
		plain, err := r.enc.DecryptString(text)
		if err != nil {
			// NOT this application's ciphertext — see isCiphertextEnvelope. Export it verbatim and
			// record the miss; the document-level rule in exportKeyHealthError decides whether the
			// misses add up to a broken key rather than an unlucky string.
			tally.miss(table, column, err)
			continue
		}
		tally.hit()
		row[column] = plain
	}
}

// envelopeTally records how the whole document's envelope-shaped strings resolved.
//
// ⚠ WHY THIS IS DOCUMENT-LEVEL AND NOT PER-VALUE, which is the entire #1587 fix-round argument.
// Per value, an authentication failure is genuinely ambiguous: it is either a user string that
// collided with the heuristic, or this application's own ciphertext under a key that no longer
// opens it. Failing loudly on both (the first draft) hands a user a 500 for their whole Article 20
// request because they once typed "ENC$" in front of something base64-shaped. Passing through
// silently on both restores the round-4 defect — a document full of unreadable "ENC$…" strings,
// shipped as if it were the user's data, with nothing anywhere saying so.
//
// Across a WHOLE export the two stop being ambiguous FOR A WHOLLY WRONG KEY. Such a key fails EVERY
// envelope, including users.email, which profile_repository encrypts for every account this
// application has ever created; a colliding user string fails while its neighbours succeed. So: at
// least one success anywhere in the document proves the key opens this user's data, and the
// failures beside it are strings the user typed. Zero successes with at least one failure is the
// key alarm, and it is still loud.
//
// ⚠ ACCEPTED LIMIT, AND THE RECORDED VERSION OF IT WAS TOO COMFORTABLE (#1569 T8.5). It used to be
// filed as "unreachable — no rotation machinery in internal/crypto/". The Go half of that is true
// and re-verified: no `rotat*` anywhere under internal/crypto, one key on FieldEncryptor, one
// ENCRYPTION_KEY read in config. But ROTATION NEEDS NO CODE. ENCRYPTION_KEY is an environment
// variable (config.go, fly.staging.toml, docs/SECRETS_REFERENCE.md); changing it is a deploy, not a
// commit, and from that moment every row written after the change decrypts while every row written
// before it does not, with no re-encryption tool anywhere in the tree to finish the job. The
// codebase already treats a rotated key as a LIVE operational case elsewhere — see
// plaid_orphaned_item_repository.go and domain/plaid_orphaned_item.go, which handle an
// undecryptable access token rather than declaring it impossible.
//
// Under that MIXED-KEY state this rule is wrong in the unsafe direction: one post-rotation row that
// decrypts (a freshly linked item, a just-edited note) sets decrypted > 0 and suppresses the alarm
// for every pre-rotation failure, so the export ships raw "ENC$…" ciphertext as the user's data
// with nothing saying so — the round-4 defect, reachable again by an ops action.
//
// ⚠ DELIBERATE DECISION #1569 T8.5: the gate is NOT tightened here, and the reason is not
// inertia. Every tightening available needs a list of columns that are always this application's
// ciphertext, and decryptEveryEnvelope was rewritten in #1587 precisely to delete that list — a
// column left off it was the defect the shape test replaced. Reintroducing the list to catch a
// state no deployment has ever been in would trade a known-reachable regression for a
// not-yet-reachable one. What is required instead is the operational rule this comment now records:
// ROTATING ENCRYPTION_KEY WITHOUT A RE-ENCRYPTION BACKFILL IS NOT A SUPPORTED OPERATION. If key
// rotation ever becomes a real feature, it must land with a re-encryption backfill AND a per-column
// key-health signal, and this rule must be revisited in the same change.
type envelopeTally struct {
	decrypted int
	failed    int
	firstErr  error
	firstAt   string
}

func (t *envelopeTally) hit() { t.decrypted++ }

func (t *envelopeTally) miss(table, column string, err error) {
	t.failed++
	if t.firstErr == nil {
		t.firstErr, t.firstAt = err, table+"."+column
	}
}

// exportKeyHealthError reports the one case in which an authentication failure cannot be read as a
// user-typed string: nothing in the entire document decrypted. See envelopeTally.
func (t *envelopeTally) exportKeyHealthError() error {
	if t.failed == 0 || t.decrypted > 0 {
		return nil
	}
	return fmt.Errorf(
		"ExportUserData: %d enveloped value(s) failed authentication and none succeeded, "+
			"refusing to hand the user ciphertext as if it were their data (first %s): %w",
		t.failed, t.firstAt, t.firstErr)
}

// isCiphertextEnvelope reports whether a value is SHAPED like one this application encrypted, as
// opposed to a user-typed string that merely begins with the marker.
//
// The test is structural rather than a name list: FieldEncryptor.EncryptString emits
// "ENC$" + base64.StdEncoding(nonce ‖ ciphertext ‖ GCM tag), so a genuine envelope always decodes
// as standard base64 to at least nonce+tag bytes. A note reading "ENC$ reminder to self" does not,
// and never reaches the decryptor at all.
//
// ⚠ IT IS A CHEAP PRE-FILTER, NOT THE DISCRIMINATOR, and #1587's fix round is the reason the
// distinction is spelled out. Every predicate available here is one a user can satisfy by typing:
// "ENC$" is four characters, and base64 of 28+ bytes is a string anyone can paste out of another
// system's export. The only property a user CANNOT forge is the GCM tag, because producing one
// requires this application's key — so authentication decides, in decryptEveryEnvelope, and a value
// that fails it is by construction not ours and is exported exactly as the user wrote it. Tightening
// the shape test further would only shrink the collision set; it can never empty it.
func isCiphertextEnvelope(value string) bool {
	if !appcrypto.IsEncrypted(value) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, dataExportCiphertextPrefix))
	return err == nil && len(raw) >= dataExportMinCiphertextBytes
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
