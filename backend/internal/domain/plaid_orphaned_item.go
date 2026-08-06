package domain

import "time"

// OrphanedItemTokenRetention is the retention deadline on a Plaid access token retained past its
// owner's GDPR erasure — the single number this feature's legal defensibility rests on. Issue #1497.
//
// ⚠ WHAT THE DEADLINE GUARANTEES, IN TWO PARTS OF DIFFERENT STRENGTH. Keeping them distinct is the
// point, because a mechanism-independent claim was previously made for both and only one of them holds:
//
//   - USE stops exactly at the deadline. No expired credential is ever presented to Plaid: TokenIsLive
//     below is strict at the boundary, GetRetryablePage selects on `token_expires_at > asOf`, and the
//     retry sweep re-checks against a fresh clock per row immediately before every call. None of that
//     waits for a purge to have run.
//   - EXISTENCE ends at the first purge sweep at or after the deadline — so at most
//     OrphanedItemTokenPurgeInterval (1 hour) past it, and longer only in the case where one run's
//     1,000-row cap is reached and defers the remainder to the next run.
//
// It is therefore NOT correct to say the credential cannot exist after this instant, and this comment
// used to say so ("HARD TTL"), as did migration 000135's column comment. The purge is unconditional and
// independent of Plaid configuration, which is the load-bearing property — but unconditional is not
// instantaneous, and the whole defensibility of retaining a credential past an erasure rests on this
// deadline being stated as it is actually kept. An overstated guarantee is worse than an accurate bound.
//
// WHY 30 DAYS, and why this particular 30 days. It is the account-deletion grace period this system
// already documents, operates and tells users about (config.DeletionGracePeriodDays, default 30). The
// retention therefore does not extend the outer boundary of "how long BoostedBudget holds data about a
// departing user" beyond one the codebase can already defend; it reuses it. A fresh number — 14, 45,
// 90 — would have to be argued from nothing.
//
// ⚠ NOT DERIVED FROM config.DeletionGracePeriodDays, deliberately, even though it equals its default.
// That value is an operator-tunable env var (DELETION_GRACE_PERIOD_DAYS); wiring this to it would mean
// an operator lengthening the grace period silently lengthens how long erased users' bank credentials
// are retained, which is a policy change disguised as an ops change. The two are equal by ARGUMENT,
// not by reference, and TestOrphanedItemTokenRetention_IsThirtyDays pins the value.
//
// ⚠ NOT the 7-day C2 cooling-off window either (uncleanedLinkedItemExistsClause,
// profile_repository.go). Two clocks, two purposes: that one bounds how long an ERASURE WAITS for a
// failed removal to keep being retried; this one bounds how long a CREDENTIAL OUTLIVES an erasure.
// Conflating them is the kind of coupling this branch keeps having to unpick, so
// TestOrphanedItemTokenRetention_IsNotTheC2CoolingOffWindow fails if anyone tries.
const OrphanedItemTokenRetention = 30 * 24 * time.Hour

// OrphanedItemTokenPurgeInterval is how often the unconditional purge sweep runs, and therefore the
// UPPER BOUND on how long a retained credential can still EXIST past OrphanedItemTokenRetention.
// Issue #1497.
//
// It lives here, beside the retention it bounds, rather than only in the job registration, because it
// is quoted in the guarantee itself — migration 000135's token_expires_at comment, the ADR, and
// OrphanedItemTokenRetention above all state the bound numerically, and a number stated in four places
// and defined in none of them is a number that drifts. cmd/server/periodic_jobs.go's registration and
// the purge job's UniqueOpts.ByPeriod both read it, and
// TestBuildPeriodicJobSpecs_PurgeRunsHourlyAndItsDedupDoesNotSuppressThat asserts the spec the builder
// produces carries this value and a literal one hour, so the two cannot agree vacuously.
//
// WHY ONE HOUR AND NOT THE DAY IT WAS. The exposure this bounds is a bank credential belonging to an
// erased user sitting in the database past a documented deadline, so the bound wants to be small. The
// cost of making it small is nil in this case, and that is checkable rather than assumed: migrations
// 000134 and 000135 both argue explicitly that this table holds SINGLE DIGITS OF ROWS (it takes a
// permanently-unremovable Plaid Item *and* a completed erasure to make one), which is why they
// deliberately create no index for either sweep to use. An hourly sequential scan of single digits of
// rows is 24 scans a day of essentially nothing, and the per-run cap of 1,000 rows
// (maxOrphanTokenPurgesPerSweep) stays unreachable at that size — reaching it is itself the alarming
// signal 000134 named. Shortening the interval therefore tightens the bound without weakening the cap.
//
// ⚠ NOT the retry sweep's cadence, which stays daily. That sweep is nil-guarded on Plaid being
// configured and OrphanedItemRetryStaleAfter below is derived from it (48 hours = two of its cadences);
// this one is unconditional and must stay a separate number. Coupling them is the mistake this branch
// keeps having to unpick.
//
// ⚠ IF THIS VALUE CHANGES, every sentence quoting the bound changes with it: this file, migration
// 000135's token_expires_at column comment, PurgeExpiredTokens' doc comment, and
// docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md § "Plaid orphaned items".
const OrphanedItemTokenPurgeInterval = 1 * time.Hour

// OrphanedItemRetryStaleAfter is how long a retained token may sit WITHOUT an observed retry before
// the record stops claiming that something automated is working on it. Issue #1497.
//
// THIS CONSTANT IS THE ANSWER TO A SPECIFIC QUESTION: can a row say "auto-retrying" while nothing is
// retrying? Without it, yes — trivially. "Auto-retrying" is a claim about a JOB, and a database row
// cannot see whether that job is registered, wired, leader-elected, or starved behind a backlog. The
// retry sweep is also nil-guarded on Plaid being configured (it needs a Plaid client), so a deployment
// with Plaid unconfigured would produce rows asserting an automated retry that structurally cannot
// exist. That is this branch's signature defect — state marked, work never happened, mark survives
// forever — and it would have been its sixth instance.
//
// So the claim DECAYS instead of being asserted. 48 hours = two cadences of the daily retry sweep, so
// a single missed run (deploy, leader election, one-off failure) does not raise a false alarm, while a
// sweep that is not running at all is reported within two days rather than after thirty. The error
// direction is deliberate: over-reporting manual work costs an operator one look at a row that turns
// out to be fine; under-reporting it costs a live Plaid Item billing for a month behind a green label.
const OrphanedItemRetryStaleAfter = 48 * time.Hour

// OrphanTokenUndecryptableCode is the LOCAL sentinel recorded in
// plaid_orphaned_items.last_retry_error_code when the retained ciphertext could not be decrypted —
// in practice an encryption-key rotation between the erasure and the retry. Not a Plaid error code,
// and spelled in a shape Plaid's vocabulary does not use so nobody mistakes it for one (the same
// convention as services' NO_USABLE_ACCESS_TOKEN sentinel).
//
// WHY IT IS NOT TREATED AS "PURGE IT NOW", which was the first design and is wrong: a missing or
// misconfigured key is an OPERATIONS problem and is frequently transient (a bad deploy, an unset env
// var on one machine). Purging on the first decrypt failure would destroy a recoverable credential
// across every orphan row on the first sweep after such a deploy, irreversibly. The token is therefore
// KEPT until its own deadline — but the row stops claiming to be auto-retrying (AdminState treats this
// code as terminal), so it is never a silently-rotting green row, and if the key comes back a later
// retry clears the code and the row returns to auto-retrying on its own.
const OrphanTokenUndecryptableCode = "ORPHAN_TOKEN_UNDECRYPTABLE"

// OrphanedItemAdminState is what an operator must DO about an orphan record — deliberately a
// derivation over the row rather than a stored column, so it cannot drift from the facts it summarises
// and so no writer has to remember to update it. Issue #1497.
type OrphanedItemAdminState string

const (
	// OrphanedItemResolved: the Item was proved removed at Plaid by an automated retry. Kept as
	// history. The only state that means nobody needs to do anything.
	OrphanedItemResolved OrphanedItemAdminState = "resolved"
	// OrphanedItemAutoRetrying: a live, unexpired, decryptable credential is held and a retry has been
	// observed recently enough to believe the sweep is working. No operator action — acting at Plaid
	// now would duplicate work that is in flight.
	OrphanedItemAutoRetrying OrphanedItemAdminState = "auto_retrying"
	// OrphanedItemManualInterventionRequired: nothing automated will finish this. Somebody must remove
	// the Item in Plaid's dashboard using plaid_item_id, or it may keep billing. Reached by expiry, by
	// purge, by an undecryptable token, by a row that never held one, and — importantly — by a live
	// token that nothing has retried for OrphanedItemRetryStaleAfter.
	OrphanedItemManualInterventionRequired OrphanedItemAdminState = "manual_intervention_required"
)

// PlaidOrphanedItem is one row of plaid_orphaned_items: a Plaid Item that may still exist and may
// still bill, whose local linked_items row was destroyed by a GDPR erasure before removal could be
// proved. It carries no user id and no user PII by design — see migration 000134 — plus, since
// migration 000135, a hard-bounded copy of the Item's access token.
//
// PlaidItemID / InstitutionID / InstitutionName are the DECRYPTED values; the repository decrypts on
// read through the same FieldEncryptor that produced the ciphertext. AccessToken is likewise plaintext
// and is "" whenever no credential is held OR the ciphertext could not be decrypted — the caller
// distinguishes those via TokenPurgedAt and the retry sweep's own error handling, never by guessing.
type PlaidOrphanedItem struct {
	ID              string
	LinkedItemID    string
	PlaidItemID     string
	InstitutionID   string
	InstitutionName string
	// LinkedItemStatus is linked_items.status at the moment of erasure. 'replaced' means the Item was
	// a re-link leftover whose /item/remove was never attempted — the leg for which a retry is not
	// merely worth trying but is the only automated path that has ever existed.
	LinkedItemStatus string
	// LastErrorCode is the removal error code AT THE MOMENT OF ERASURE. Never overwritten by a retry —
	// LastRetryErrorCode carries that, so the diagnostic explaining why the row exists is not lost.
	LastErrorCode string
	TerminalSince *time.Time
	OrphanedAt    time.Time
	AccessToken   string
	// TokenUndecryptable is true when a credential IS stored but this process could not decrypt it —
	// in practice an encryption-key rotation. Distinct from AccessToken == "" with this false, which
	// means no credential is stored at all: one is "we have it and cannot use it", the other is "we do
	// not have it". Both mean no retry can succeed right now; only the first can start working again on
	// its own if the key comes back.
	TokenUndecryptable bool
	TokenExpiresAt     time.Time
	TokenPurgedAt      *time.Time
	RetryAttemptCount  int
	LastRetryAt        *time.Time
	LastRetryErrorCode string
	ResolvedAt         *time.Time
}

// TokenIsLive reports whether this record holds a credential that may still be presented to Plaid at
// asOf. It is the expiry half of the retention bargain expressed once, so the admin derivation below
// and any caller that reads a token cannot disagree about the boundary.
//
// The comparison is strict (`asOf.Before(TokenExpiresAt)`): AT the deadline the token is spent. This
// is deliberately independent of whether the purge sweep has actually run — the deadline is the rule,
// the purge is the enforcement, and code that reads a token must not wait for the enforcement.
//
// A record whose ciphertext would not decrypt reports false here too, because the repository leaves
// AccessToken empty in that case (and sets TokenUndecryptable to say why). "Live" means presentable to
// Plaid, and an undecryptable value is not.
func (o PlaidOrphanedItem) TokenIsLive(asOf time.Time) bool {
	return o.AccessToken != "" && asOf.Before(o.TokenExpiresAt)
}

// AdminState derives which of the two operator stories (or neither, when resolved) this row is in.
// asOf is taken rather than read from the clock so the expiry and staleness boundaries are testable —
// Issue #1466's rule, applied here from the start.
//
// The order of the checks is the priority order and each is load-bearing:
//
//  1. Resolved wins outright. A removed Item is not work, whatever its token or attempt history says.
//  2. A dead token (absent, purged, or past its deadline) means manual intervention. This is the state
//     the table exists to surface.
//  3. A token this process could not decrypt is already covered by (2) — the repository leaves
//     AccessToken empty and sets TokenUndecryptable, so TokenIsLive is false. This third check is
//     narrower and not redundant with it: it catches the row whose LAST RETRY could not decrypt even
//     though the reader can, which is what a key rolled out to some processes and not others looks
//     like. See OrphanTokenUndecryptableCode.
//  4. A live token with no OBSERVED retry within OrphanedItemRetryStaleAfter means manual intervention:
//     the row must not claim an automated retry it has no evidence of. See that constant.
//  5. Only then: auto-retrying.
//
// RetryAttemptCount is deliberately absent from this derivation. Nothing in this system gives up on an
// attempt count, so a high count is information for the operator, never a state.
func (o PlaidOrphanedItem) AdminState(asOf time.Time) OrphanedItemAdminState {
	if o.ResolvedAt != nil {
		return OrphanedItemResolved
	}
	if !o.TokenIsLive(asOf) {
		return OrphanedItemManualInterventionRequired
	}
	if o.LastRetryErrorCode == OrphanTokenUndecryptableCode {
		return OrphanedItemManualInterventionRequired
	}
	// The staleness horizon runs from the last retry if there has ever been one, and otherwise from
	// when the row was written — a row has to be given at least one cadence to be picked up before its
	// silence means anything.
	lastSignal := o.OrphanedAt
	if o.LastRetryAt != nil {
		lastSignal = *o.LastRetryAt
	}
	if asOf.Sub(lastSignal) >= OrphanedItemRetryStaleAfter {
		return OrphanedItemManualInterventionRequired
	}
	return OrphanedItemAutoRetrying
}

// PlaidOrphanedItemAdminView is one row as the admin API exposes it. Issue #1497.
//
// ⚠ IT HAS NO ACCESS-TOKEN FIELD, AND MUST NEVER GAIN ONE. The retained credential is the whole
// sensitivity of this table: it is a live capability against a third party, and an admin endpoint is
// exactly the wrong place for it — nothing an operator does needs the token, because the manual remedy
// is performed in Plaid's own dashboard using plaid_item_id. TokenRetained answers the only question a
// human has about it ("is something still able to try?"), as a boolean.
// TestPlaidOrphanedItemAdminView_NeverSerialisesTheCredential fails if a field ever carries it.
//
// AdminState is computed, not stored (see AdminState), so this view cannot disagree with the row.
type PlaidOrphanedItemAdminView struct {
	ID          string `json:"id"`
	PlaidItemID string `json:"plaid_item_id"`
	// AdminState is what an operator must DO: "auto_retrying" (nothing — work is in flight),
	// "manual_intervention_required" (remove this Item in Plaid's dashboard, or it may keep billing),
	// or "resolved" (history).
	AdminState      OrphanedItemAdminState `json:"admin_state"`
	InstitutionID   string                 `json:"institution_id,omitempty"`
	InstitutionName string                 `json:"institution_name,omitempty"`
	// LinkedItemStatusAtErasure distinguishes a 'replaced' re-link leftover, whose removal was never
	// attempted, from an 'active' row whose removal was attempted and failed. Different operator stories.
	LinkedItemStatusAtErasure string `json:"linked_item_status_at_erasure,omitempty"`
	// ErasureErrorCode is the code that made removal terminal at the moment of erasure; never overwritten.
	ErasureErrorCode string `json:"erasure_error_code,omitempty"`
	// LastRetryErrorCode is the most recent automated attempt's failure code, including the local
	// sentinels ORPHAN_TOKEN_UNDECRYPTABLE and ORPHAN_RETRY_TRANSPORT_ERROR.
	LastRetryErrorCode string     `json:"last_retry_error_code,omitempty"`
	RetryAttemptCount  int        `json:"retry_attempt_count"`
	OrphanedAt         time.Time  `json:"orphaned_at"`
	TerminalSince      *time.Time `json:"terminal_since,omitempty"`
	LastRetryAt        *time.Time `json:"last_retry_at,omitempty"`
	// TokenExpiresAt is the retention deadline. TokenRetained reports whether a credential is actually
	// held right now; TokenPurgedAt records when one was destroyed, and is null on a record that never
	// held one (rows written before migration 000135) — never-retained and purged are different facts.
	TokenExpiresAt time.Time  `json:"token_expires_at"`
	TokenPurgedAt  *time.Time `json:"token_purged_at,omitempty"`
	TokenRetained  bool       `json:"token_retained"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

// AdminView projects the record for the admin API, computing AdminState against asOf.
//
// TokenRetained is deliberately `AccessToken != "" || TokenUndecryptable` — i.e. "a credential is
// stored", not "a credential is usable". An undecryptable token IS retained personal data and must be
// reported as retained; reporting it as absent would understate what this system holds, which is the
// one direction a compliance surface must never err in. The AdminState beside it already says the
// record needs a human.
func (o PlaidOrphanedItem) AdminView(asOf time.Time) PlaidOrphanedItemAdminView {
	return PlaidOrphanedItemAdminView{
		ID:                        o.ID,
		PlaidItemID:               o.PlaidItemID,
		AdminState:                o.AdminState(asOf),
		InstitutionID:             o.InstitutionID,
		InstitutionName:           o.InstitutionName,
		LinkedItemStatusAtErasure: o.LinkedItemStatus,
		ErasureErrorCode:          o.LastErrorCode,
		LastRetryErrorCode:        o.LastRetryErrorCode,
		RetryAttemptCount:         o.RetryAttemptCount,
		OrphanedAt:                o.OrphanedAt,
		TerminalSince:             o.TerminalSince,
		LastRetryAt:               o.LastRetryAt,
		TokenExpiresAt:            o.TokenExpiresAt,
		TokenPurgedAt:             o.TokenPurgedAt,
		TokenRetained:             o.AccessToken != "" || o.TokenUndecryptable,
		ResolvedAt:                o.ResolvedAt,
	}
}
