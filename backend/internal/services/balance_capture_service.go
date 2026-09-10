package services

// Periodic balance capture (#1626 Phase 3, Defect 3 / Task G).
//
// THE DEFECT. balance_snapshots is what the net-worth and balance-history charts read, and until
// this file existed it was written ONLY as a side effect of a transaction sync — which runs on
// initial link and on an inbound Plaid TRANSACTIONS webhook, and nowhere else. No periodic
// balance-snapshot job existed and no scheduled plaid_sync existed. No backfill exists, and none is
// being built. So a day with no webhook was a day with no row, permanently.
//
// WHAT THIS IS. A gap filler: for one linked item, ensure every ACTIVE account has a balance
// snapshot for the current UTC day, and do nothing at all if they already do. It is deliberately
// NOT an unconditional daily writer — see CaptureItemBalances for why that distinction is the whole
// cost argument.
//
// WHAT THIS IS NOT.
//
//   - It is not a reconciliation pass. transactionSyncService.createBalanceSnapshots removes and
//     restores accounts as a side effect of reading the roster; this path does NOT, and that is
//     deliberate rather than an omission. It retires a whole risk class: a daily roster read that
//     cannot soft-remove anything has no partial-roster hazard and raises no account-flapping
//     question. REMOVING the side effect beats guarding it. The sync path keeps its reconciliation
//     by wrapping the shared cohort writer below; this one calls it plain.
//   - It is not a source of fabricated observations. There is no fallback to
//     accounts.current_balance: that column is written only at link time, ordinary account updates
//     never refresh it, and real syncs write balances into balance_snapshots without touching it —
//     so it is stale-since-link. Worse, every read takes the latest snapshot_at, so a fabricated
//     same-day row would BEAT a genuine earlier webhook row. A gap is recoverable information; an
//     invented observation that outranks a real one is not. River retries; an honest gap is left.
//   - It does not bump the cache. That is the WORKER's job (queue/jobs/balance_snapshot_capture.go),
//     for the same reason the sync's bump lives in SyncTransactions rather than in the writer: the
//     bump belongs where the unit of work ends.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/plaid/plaid-go/v23/plaid"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// BalanceCaptureOutcome is what one item's capture did, in enough detail for an operator to tell the
// four "nothing happened" cases apart. They are genuinely different and collapsing them to a bool
// would make the job unobservable:
//
//	CohortComplete — every active account already had today's row; no provider call was made.
//	Ineligible     — the item is in a needs-reauth or terminal state, or is not this user's.
//	Written == 0 with neither of the above — the roster produced no usable balance at all.
//
// ⚠ ProviderAbsent is the FOURTH, and unlike the three above it is never a "nothing happened" case
// that returns success: a non-zero ProviderAbsent always accompanies ErrBalanceCaptureAccountAbsent.
// It is on this struct so the condition is COUNTABLE rather than only present in an error string
// (#1626 gap G8).
type BalanceCaptureOutcome struct {
	// Day is the UTC calendar day this run was about, carried so the caller logs the same day the
	// cohort question was asked about rather than re-deriving it from its own clock.
	Day time.Time
	// CohortComplete reports the skip that makes the daily cadence free. When true, PlaidCalled is
	// false and no row was written.
	CohortComplete bool
	// Ineligible reports a refusal that retrying cannot fix.
	Ineligible bool
	// PlaidCalled records whether /accounts/get was actually invoked. Exists so the cost property is
	// observable in production logs, not only in tests.
	PlaidCalled bool
	// Written counts rows this run inserted. AlreadyPresent counts the ones a concurrent writer had
	// already inserted for the day (a normal outcome, not an error).
	Written        int
	AlreadyPresent int
	// Unmatched counts provider accounts with no ACTIVE internal row — a removal we already made, or
	// a roster row not yet linked. NoBalance counts accounts the provider reported with neither a
	// current nor an available balance, which cannot become a row.
	Unmatched int
	NoBalance int
	// Deduped counts accounts skipped because an identical snapshot already exists inside the shared
	// deduplication window.
	Deduped int
	// ProviderAbsent counts ACTIVE internal accounts that had no row for the day AND were not named
	// in the provider's roster at all (#1626 gap G8).
	//
	// It is the inverse of Unmatched: that one counts provider accounts with no active internal row,
	// this one counts active internal accounts with no provider entry. The second direction used to
	// be invisible — writeBalanceCohort walks the provider's roster, so an internal account the
	// provider did not report was never visited and produced no row, no counter and no error.
	//
	// A non-zero value ALWAYS accompanies an error from CaptureItemBalances; it is counted rather
	// than merely reported so the condition is measurable in production instead of only being
	// inferable from the gap between the gaps_found and written log fields.
	ProviderAbsent int
}

// ErrBalanceCaptureAccountAbsent is returned when at least one ACTIVE internal account had no
// snapshot for the day and the provider's /accounts/get response did not name it (#1626 gap G8).
//
// Its own error, not a generic one, because the worker logs it and an operator has to be able to
// tell it apart from a provider outage: an outage is a transient the retry will fix, whereas this
// says our records and the provider's disagree about which accounts exist, and the resolution is the
// SYNC path's reconciliation (which soft-removes what a roster proves absent) rather than anything
// this job can do.
var ErrBalanceCaptureAccountAbsent = errors.New("balance capture: active account absent from the provider roster")

// BalanceCaptureService fills the current UTC day's balance-snapshot gap for ONE linked item.
//
// One item per call, because that is the unit River can retry in isolation. A single job looping
// every item would make one item's provider failure either abort the rest or be swallowed, and
// swallowing is the failure mode this whole task exists to remove.
type BalanceCaptureService interface {
	// CaptureItemBalances ensures every active account of itemID has a balance snapshot for the
	// current UTC day.
	//
	// userID is the owner the caller believes the item has; a mismatch is refused.
	//
	// Returns an error ONLY for conditions a retry could plausibly fix (a provider failure, a
	// database failure, a partial write). An ineligible item and a complete cohort are reported
	// through the outcome and return nil, because retrying either is pure waste.
	CaptureItemBalances(ctx context.Context, itemID, userID string) (BalanceCaptureOutcome, error)
}

// BalanceCaptureServiceDeps are the dependencies NewBalanceCaptureService needs. All are required;
// there is no nil-guard, because every one of them is load-bearing and a nil-tolerant capture would
// silently become a no-op scheduled to run every day.
type BalanceCaptureServiceDeps struct {
	LinkedItemRepo      repository.LinkedItemRepository
	AccountRepo         repository.AccountRepository
	BalanceSnapshotRepo repository.BalanceSnapshotRepository
	PlaidService        PlaidService
	Logger              *slog.Logger
}

type balanceCaptureService struct {
	items     repository.LinkedItemRepository
	accounts  repository.AccountRepository
	snapshots repository.BalanceSnapshotRepository
	plaid     PlaidService
	logger    *slog.Logger
}

// NewBalanceCaptureService constructs the periodic balance-capture service.
func NewBalanceCaptureService(deps BalanceCaptureServiceDeps) BalanceCaptureService {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &balanceCaptureService{
		items:     deps.LinkedItemRepo,
		accounts:  deps.AccountRepo,
		snapshots: deps.BalanceSnapshotRepo,
		plaid:     deps.PlaidService,
		logger:    logger,
	}
}

// balanceCaptureEligible reports whether a linked-item status is one a daily capture will act on.
//
// Exactly one, and the narrowness is the point. Every other status is either a needs-reauth state
// (login_required, error, pending_expiration, revoked) whose Plaid call would fail today and fail
// again tomorrow — ITEM_LOGIN_REQUIRED heals when the user re-authenticates, not on retry — or a
// terminal one (replaced, disconnected) with nothing to snapshot. This reads the EXISTING
// item-error state rather than inventing a second notion of "broken", so an item regains eligibility
// exactly when the product already considers it recovered.
func balanceCaptureEligible(status domain.LinkedItemStatus) bool {
	// A legacy NULL status normalises to active upstream (normalizeLinkedItemStatus), so the empty
	// string is treated the same way here rather than being silently ineligible forever.
	return status == domain.LinkedItemStatusActive || status == ""
}

// CaptureItemBalances — see the interface for the contract.
//
// ORDERING IS LOAD-BEARING: the cohort question comes FIRST, before the item is even read.
//
// The design's entire cost argument is that an item a webhook has already covered today costs ZERO
// incremental Plaid calls, and that is only true if the check precedes the call. It also means the
// common path does no access-token decryption at all.
//
// ⚠ THE PREDICATE IS "FULL COHORT", NOT "ANY ROW TODAY". A webhook that snapshotted 3 of an item's 5
// accounts leaves the incomplete-cohort defect in place for that day — the day's aggregate was the
// sum of only the accounts that received a row — so a partial day must not count as done. That is
// why AccountsMissingDailySnapshot returns the missing accounts rather than a boolean.
//
// ⚠ THE DAY IS A UTC CALENDAR DAY, and this is stated in code because the job runs at a fixed UTC
// hour that lands at a different LOCAL time for every user: a UTC-11 user's 21:00-UTC row is stamped
// with a date that is still "tomorrow" for them. For a daily balance series that is acceptable.
// Silently ambiguous day boundaries are not, which is why nothing on this path consults a local
// clock.
func (s *balanceCaptureService) CaptureItemBalances(ctx context.Context, itemID, userID string) (BalanceCaptureOutcome, error) {
	now := time.Now().UTC()
	out := BalanceCaptureOutcome{Day: now}

	missing, err := s.snapshots.AccountsMissingDailySnapshot(ctx, itemID, now)
	if err != nil {
		return out, fmt.Errorf("balance capture: cohort check for item %s: %w", itemID, err)
	}
	if len(missing) == 0 {
		out.CohortComplete = true
		s.logger.Debug("balance capture: cohort already complete for the UTC day, no provider call",
			"item_id", itemID, "day", now.Format("2006-01-02"))
		return out, nil
	}

	item, err := s.items.GetByID(ctx, itemID)
	if err != nil {
		if errors.Is(err, repository.ErrLinkedItemNotFound) {
			// The item was deleted between enumeration and execution. Nothing to capture and
			// nothing a retry could recover.
			out.Ineligible = true
			return out, nil
		}
		return out, fmt.Errorf("balance capture: read item %s: %w", itemID, err)
	}
	if item == nil {
		out.Ineligible = true
		return out, nil
	}
	if item.UserID != userID {
		// The item changed hands, or these args reached this worker through a dead-letter replay.
		// Either way, refuse rather than read one user's balances under another user's job — and
		// refuse WITHOUT an error, because no retry can make an item change owner.
		s.logger.Warn("balance capture: item ownership mismatch, skipping",
			"item_id", itemID, "job_user_id", userID)
		out.Ineligible = true
		return out, nil
	}
	if !balanceCaptureEligible(item.Status) {
		// A SECOND line of defence, not a redundant one. The dispatcher filters on status at
		// enumeration time, but minutes pass before this runs, River retries a capture for its whole
		// budget, and a dead-letter replay can resurrect one much later.
		s.logger.Info("balance capture: item is not in a capturable state, skipping",
			"item_id", itemID, "status", item.Status)
		out.Ineligible = true
		return out, nil
	}

	// /accounts/get, and NEVER /accounts/balance/get. The former returns Plaid's own CACHED balances
	// and is what this tree's sync path already uses; the latter forces a read from the institution
	// and carries per-call Balance-product billing. It appears nowhere in this tree and must stay
	// that way.
	// ⚠ item.AccessToken, NOT item.AccessTokenEncrypted. The repository populates the plaintext field
	// by decrypting the stored one on every read (decryptLinkedItemPIIFields); sending the ciphertext
	// would fail at Plaid with INVALID_ACCESS_TOKEN — a permanent-looking error for a purely local
	// mistake, retried daily forever. Pinned by
	// TestCaptureItemBalances_SendsTheDecryptedAccessToken.
	client := s.plaid.GetClient()
	req := plaid.NewAccountsGetRequest(item.AccessToken)
	out.PlaidCalled = true
	resp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*req).Execute()
	if err != nil {
		return out, fmt.Errorf("balance capture: /accounts/get for item %s: %w", itemID, err)
	}

	plaidAccounts := resp.GetAccounts()
	if len(plaidAccounts) == 0 {
		// A response naming no accounts is the outermost of this codebase's three guards against a
		// zero-account sweep. Nothing downstream runs on it. Not an error: an item can legitimately
		// have had every account closed, and failing here would retry daily forever.
		//
		// ⚠ THE G8 ABSENCE CHECK BELOW DELIBERATELY DOES NOT COVER THIS BRANCH, and the boundary is
		// the same one the three zero-account guards draw. G8 concludes "the provider says this
		// account is gone" from a roster that named OTHER accounts — a roster this codebase treats
		// as reliable. A roster that names NOTHING is the shape PlaidAccountRosterFrom marks
		// INCOMPLETE and reconcileRemovedAccounts refuses to act on, precisely because it cannot be
		// distinguished from a provider-side fault. Drawing an absence conclusion from it would be
		// exactly what those guards exist to forbid. So this path stays a logged non-error, and the
		// accounts the cohort check named stay uncaptured for the day — a residual, recorded here
		// rather than closed, because closing it would mean trusting a response the rest of this
		// tree does not trust.
		s.logger.Info("balance capture: provider returned no accounts, nothing to snapshot", "item_id", itemID)
		return out, nil
	}

	// The retained-INCLUSIVE read, filtered here. Reading the active-only view would be equivalent in
	// production and UNTESTABLE: a test could not then hand this function a removed account the
	// provider still reports, and "the removed account got no row" would pass vacuously. The filter
	// is activeAccounts, the same one createBalanceSnapshots uses, so the two paths cannot drift on
	// what "active" means — and a removed account accruing NO snapshot is what stops a
	// user-disconnected account quietly resuming data collection because the bank still reports it.
	all, err := s.accounts.GetByLinkedItemIDIncludingRetained(ctx, itemID)
	if err != nil {
		return out, fmt.Errorf("balance capture: read accounts for item %s: %w", itemID, err)
	}

	result, writeErr := writeBalanceCohort(ctx, balanceCohortInput{
		logger:         s.logger,
		linkedItem:     item,
		plaidAccounts:  plaidAccounts,
		activeAccounts: activeAccounts(all),
		source:         domain.SnapshotSourceDailyCapture,
		capturedAt:     now,
		dedupWindow:    balanceSnapshotDeduplicationWindow,
		latest:         s.snapshots.GetLatestByAccountID,
		insert:         s.snapshots.CreateDailyCapture,
	})
	out.Written = result.Written
	out.AlreadyPresent = result.AlreadyPresent
	out.Unmatched = result.Unmatched
	out.NoBalance = result.NoBalance
	out.Deduped = result.Deduped

	if writeErr != nil {
		// LOUD, deliberately. The writer this was extracted from logged and swallowed individual
		// insert failures while returning success, so a partial cohort committed while River recorded
		// the item as succeeded — exactly the incomplete-cohort shape that caused the reported bug.
		return out, fmt.Errorf("balance capture: item %s: %w", itemID, writeErr)
	}

	// #1626 gap G8. Every account in `missing` is one the cohort check just said has no row for the
	// day; `absent` is the subset the provider did not report at all, so writeBalanceCohort never
	// visited them and they end the run with no row, no counter and — until this check — no error.
	absent := activeAccountsAbsentFromRoster(missing, all, plaidAccounts)
	out.ProviderAbsent = len(absent)

	s.logger.Info("balance capture complete",
		"item_id", itemID,
		"day", now.Format("2006-01-02"),
		"gaps_found", len(missing),
		"written", out.Written,
		"already_present", out.AlreadyPresent,
		"deduped", out.Deduped,
		"unmatched", out.Unmatched,
		"no_balance", out.NoBalance,
		"provider_absent", out.ProviderAbsent,
	)

	if len(absent) > 0 {
		// FAILS THE ITEM, and it is the same policy this service already applies to a partially
		// written cohort — for the same reason, stated there: a job whose entire remit is "every
		// active account has a row for today" must not report success having covered some of them.
		// An account the provider did not report is a partial cohort; only the origin of the gap
		// differs.
		//
		// The rows that DID write stay written (each insert commits individually), so the failure
		// costs no completed work. What it buys is that a permanent disagreement between our records
		// and the provider's dead-letters loudly instead of accruing silence — and the resolution is
		// the sync path's reconcileRemovedAccounts, which soft-removes what a roster proves absent,
		// after which the account is no longer active and no longer part of this cohort.
		//
		// NOTHING IS FABRICATED for the absent accounts. Carrying forward a last-known balance was
		// considered and rejected on this file's own standing rule: every read takes the latest
		// snapshot_at, so an invented same-day row would OUTRANK a genuine earlier webhook row.
		s.logger.Error("balance capture: active accounts missing from the provider roster; failing the item",
			"item_id", itemID, "day", now.Format("2006-01-02"), "account_ids", absent, "written", out.Written)
		return out, fmt.Errorf("%w: item %s, accounts %v", ErrBalanceCaptureAccountAbsent, itemID, absent)
	}

	return out, nil
}

// activeAccountsAbsentFromRoster returns the ids of accounts that are (a) named by the cohort check
// as having no snapshot for the day, (b) ACTIVE in our own records, and (c) absent from the
// provider's roster.
//
// All three conditions are load-bearing, and dropping any one of them turns this into a check that
// fails healthy items:
//
//   - (a) scopes it to actual GAPS. An absent account that already has today's row is not missing
//     data — a bank that closed an account simply stops returning it, and failing on that would
//     dead-letter this job every day for an item whose day is complete.
//   - (b) excludes REMOVED accounts, which are meant to accrue no snapshots at all. Their absence
//     from the roster is the expected state, so treating it as a fault would break the daily job for
//     every item a user has ever partially disconnected. It reads `accounts` rather than trusting
//     the cohort query to have filtered them, because the two reads are separate statements and a
//     removal landing between them would otherwise produce a spurious failure.
//   - (c) is the condition itself.
//
// An id in `missing` with no matching row in `accounts` is IGNORED: the account was deleted between
// the two reads, so there is nothing to snapshot and nothing a failure could recover.
func activeAccountsAbsentFromRoster(missing []string, accounts []domain.Account, roster []plaid.AccountBase) []string {
	if len(missing) == 0 {
		return nil
	}
	reported := make(map[string]bool, len(roster))
	for _, plaidAccount := range roster {
		reported[plaidAccount.GetAccountId()] = true
	}
	byID := make(map[string]domain.Account, len(accounts))
	for _, account := range accounts {
		byID[account.ID] = account
	}

	var absent []string
	for _, id := range missing {
		account, exists := byID[id]
		switch {
		case !exists:
			continue
		case account.Status != domain.AccountStatusActive:
			continue
		case reported[account.PlaidAccountID]:
			continue
		default:
			absent = append(absent, id)
		}
	}
	return absent
}

// ─── the shared cohort writer ──────────────────────────────────────────────────────────────────

// balanceSnapshotDeduplicationWindow is the window inside which an IDENTICAL snapshot is treated as
// saying nothing new. Shared by both provenances so the two paths cannot drift.
//
// It reduces excessive snapshots; it does not bound duplication (the read and the write are separate
// statements and webhook processing takes no linked-item lock). For the daily capture, migration
// 000194's partial unique index is what actually bounds it.
const balanceSnapshotDeduplicationWindow = 5 * time.Minute

// balanceCohortInsert persists one snapshot and reports whether it inserted.
//
// A function rather than a repository, because the two provenances genuinely need different conflict
// handling and branching on a source string inside shared code would hide that. The sync path passes
// a plain Create (webhook history permits several rows per account per day, by design — migration
// 000017); the daily capture passes CreateDailyCapture, whose ON CONFLICT is scoped to migration
// 000194's partial index.
type balanceCohortInsert func(ctx context.Context, snapshot *domain.BalanceSnapshot) (bool, error)

// balanceCohortInput is one cohort write: a provider roster in, rows out.
type balanceCohortInput struct {
	logger         *slog.Logger
	linkedItem     *domain.LinkedItem
	plaidAccounts  []plaid.AccountBase
	activeAccounts []domain.Account
	source         string
	capturedAt     time.Time
	dedupWindow    time.Duration
	latest         func(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error)
	insert         balanceCohortInsert
}

// balanceCohortResult counts what happened, per disposition.
type balanceCohortResult struct {
	Written        int
	AlreadyPresent int
	Deduped        int
	Unmatched      int
	NoBalance      int
}

// writeBalanceCohort writes one balance snapshot per active account named in a provider roster.
//
// EXTRACTED FROM transactionSyncService.createBalanceSnapshots (#1626 Phase 3 Task G), which could
// not be reused as it stood: it was unexported, it called /accounts/get itself, it ran account
// removal and restore reconciliation INLINE, its provenance was hardcoded to plaid_webhook, and — the
// one that matters most — it LOGGED AND SWALLOWED individual insert failures while returning success.
//
// ⚠ THIS FUNCTION RETURNS ERRORS; IT DOES NOT DECIDE WHAT THEY MEAN. Every per-account failure is
// collected and joined, and the caller chooses. That is the whole point of the extraction: the sync
// path deliberately treats a snapshot failure as non-fatal (a transaction sync must not fail because
// a balance row did not land, and the next webhook will retry it), while the capture path treats it
// as a job failure (a job whose entire remit is "every account has a row for today" must not report
// success having written some of them). Before the extraction, ONE policy was baked in here and the
// capture path would have inherited the wrong one.
//
// It performs NO reconciliation and NO provider call. Roster in, rows out.
func writeBalanceCohort(ctx context.Context, in balanceCohortInput) (balanceCohortResult, error) {
	var result balanceCohortResult

	byPlaidID := make(map[string]domain.Account, len(in.activeAccounts))
	for _, account := range in.activeAccounts {
		byPlaidID[account.PlaidAccountID] = account
	}

	var failures []error
	for _, plaidAccount := range in.plaidAccounts {
		plaidAccountID := plaidAccount.GetAccountId()

		internalAccount, exists := byPlaidID[plaidAccountID]
		if !exists {
			// Either an account this product already removed (which must accrue no snapshots) or one
			// not yet linked. Neither is an error.
			in.logger.Warn("balance cohort: provider account has no active internal account, skipping snapshot",
				"plaid_account_id", plaidAccountID, "source", in.source)
			result.Unmatched++
			continue
		}

		balances, hasBalances := plaidAccount.GetBalancesOk()
		if !hasBalances || balances == nil {
			in.logger.Warn("balance cohort: no balance data for account, skipping snapshot",
				"plaid_account_id", plaidAccountID, "source", in.source)
			result.NoBalance++
			continue
		}

		// Built BEFORE the deduplication check, because the check needs to know what the new balance
		// IS — see isRedundantSnapshot.
		snapshot := buildBalanceSnapshot(in.logger, internalAccount, in.linkedItem, balances, in.capturedAt, plaidAccountID, in.source)
		if snapshot == nil {
			result.NoBalance++
			continue
		}

		latestSnapshot, err := in.latest(ctx, internalAccount.ID)
		if err != nil {
			// Non-fatal on purpose, and unchanged from the pre-extraction behaviour: failing to READ
			// the previous snapshot is not a reason to refuse to WRITE the new one. The cost of
			// getting this wrong is a redundant row, and the cost of the alternative is a lost day.
			in.logger.Warn("balance cohort: failed to check latest snapshot for account",
				"plaid_account_id", plaidAccountID, "error", err)
		} else if isRedundantSnapshot(latestSnapshot, snapshot, in.capturedAt, in.dedupWindow) {
			in.logger.Debug("balance cohort: skipping snapshot, an identical recent snapshot exists",
				"plaid_account_id", plaidAccountID,
				"age", in.capturedAt.Sub(latestSnapshot.SnapshotAt).Round(time.Second),
			)
			result.Deduped++
			continue
		}

		inserted, err := in.insert(ctx, snapshot)
		if err != nil {
			// COLLECTED, not swallowed, and the loop CONTINUES. Both halves matter: abandoning the
			// cohort at the first failure would lose rows this run could still have written before the
			// UTC day rolls over, and there is no backfill to repair a missed day.
			failures = append(failures, fmt.Errorf("account %s (plaid %s): %w", internalAccount.ID, plaidAccountID, err))
			continue
		}
		if inserted {
			result.Written++
		} else {
			// A lost uniqueness race: another capture already wrote this account's row for the day.
			// A normal outcome, not a failure.
			result.AlreadyPresent++
		}
	}

	if len(failures) > 0 {
		return result, fmt.Errorf("%d of %d balance snapshot writes failed: %w",
			len(failures), len(in.plaidAccounts), errors.Join(failures...))
	}
	return result, nil
}

// buildBalanceSnapshot builds a BalanceSnapshot from Plaid balance data, or nil when the provider
// reported neither a current nor an available balance.
//
// PROVENANCE IS A PARAMETER (#1626 Phase 3 Task G). It used to be hardcoded to plaid_webhook, which
// is one of the four entanglements that made the original writer unreusable — and it is not cosmetic:
// migration 000194's uniqueness key is scoped to source = 'daily_capture', so a row written with the
// wrong provenance would silently escape the idempotency constraint it was supposed to be under.
//
// ⚠ available_balance IS NOT COALESCED INTO current_balance, and that is a decision, not an
// oversight. The aggregated reads sum only current_balance and exclude a NULL from the value cohort,
// letting the per-account carry-forward supply the last known real balance. Substituting the
// available balance would be worse than a gap: on a credit card it is the remaining credit LINE, so
// it would report a large positive asset where a debt belongs.
func buildBalanceSnapshot(
	logger *slog.Logger,
	account domain.Account,
	linkedItem *domain.LinkedItem,
	balances *plaid.AccountBalance,
	timestamp time.Time,
	plaidAccountID string,
	source string,
) *domain.BalanceSnapshot {
	snapshot := &domain.BalanceSnapshot{
		AccountID:     account.ID,
		UserAccountID: account.UserAccountID,
		UserID:        linkedItem.UserID,
		SnapshotAt:    timestamp,
		Source:        source,
		CurrencyCode:  "USD",
	}

	if current, ok := balances.GetCurrentOk(); ok && current != nil {
		currentFloat := float64(*current)
		snapshot.CurrentBalance = &currentFloat
	}
	if available, ok := balances.GetAvailableOk(); ok && available != nil {
		availableFloat := float64(*available)
		snapshot.AvailableBalance = &availableFloat
	}
	if currencyCode, ok := balances.GetIsoCurrencyCodeOk(); ok && currencyCode != nil {
		snapshot.CurrencyCode = *currencyCode
	}

	if snapshot.CurrentBalance == nil && snapshot.AvailableBalance == nil {
		logger.Warn("account has neither current nor available balance, skipping snapshot",
			"plaid_account_id", plaidAccountID)
		return nil
	}
	return snapshot
}
