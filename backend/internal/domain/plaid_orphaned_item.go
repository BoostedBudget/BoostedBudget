package domain

import (
	"fmt"
	"time"
)

// OrphanedItemTokenRetention is the retention deadline on a Plaid access token retained past its
// owner's GDPR erasure — the single number this feature's legal defensibility rests on. Issue #1497.
//
// ⚠ WHAT THE DEADLINE GUARANTEES, IN TWO PARTS OF DIFFERENT STRENGTH. Keeping them distinct is the
// point, because a mechanism-independent claim was previously made for both and only one of them holds:
//
//   - USE stops exactly at the deadline. No expired credential is ever presented to Plaid: TokenIsLive
//     below is strict at the boundary, GetRetryablePage selects on `token_expires_at > asOf`, and the
//     retry sweep re-checks against a fresh clock per row immediately before every call. None of that
//     waits for a purge to have run. Note the precise scope: it is every path that could USE the
//     credential, not every path that reads the column — the admin List read deliberately does not
//     filter on the deadline, because expired rows are exactly what that surface exists to show, and
//     PlaidOrphanedItemAdminView's lack of a token field is what keeps that safe.
//   - EXISTENCE ends at the first purge sweep at or after the deadline. The bound is therefore ONE
//     SWEEP, normally OrphanedItemTokenPurgeInterval (1 hour) — and it is a bound per sweep, not an
//     absolute ceiling. It is exceeded whenever a sweep does not run or does not finish the backlog:
//     a run's 1,000-row cap deferring the remainder, downtime or a deploy window, leader election,
//     worker starvation on the low-priority queue, a failed run before River retries it, or a run
//     enqueued marginally early colliding with the previous dedup bucket (the bucket is
//     Truncate(ByPeriod) of an unaligned instant — river client.go:1750 computes the unique key before
//     ScheduledAt is assigned). ⚠ DO NOT restate this as an unconditional "at most one hour": nothing
//     here can promise successful execution, only that the credential is taken by the first sweep that
//     does run at or after the deadline. What IS unconditional is the half above — no failure of this
//     job can make the credential usable, because that check never consults this job.
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
	// OrphanedItemOperatorAttested: an operator has SAID they removed this Item in Plaid's dashboard.
	// Issue #1497 item 2.
	//
	// ⚠ IT IS A CLAIM, NOT A PROOF, AND THAT IS THE ENTIRE REASON IT IS NOT OrphanedItemResolved.
	// Resolved means an automated /item/remove came back success or ITEM_NOT_FOUND — Plaid itself
	// answered. This state means a human typed a sentence. Nothing checked it, nothing can check it
	// retroactively, and the two must stay distinguishable forever: laundering an unverified human
	// assertion into a machine-verified fact is the exact defect this branch has removed repeatedly
	// (most literally linked_items.status = 'replaced' being read as proof of removal at Plaid).
	//
	// What it therefore means to an operator: NO FURTHER MANUAL WORK IS EXPECTED — somebody already
	// did it, and their name and note are on the row — but the record stays in the table, stays
	// auditable, and (see plaidOrphanedItemRepository.GetRetryablePage) is still re-attempted by the
	// automated sweep, which is the only thing that can ever upgrade the claim to a proof.
	OrphanedItemOperatorAttested OrphanedItemAdminState = "operator_attested"
)

// AllOrphanedItemAdminStates is the complete set of values AdminState can return, in the order an
// operator cares about them: what needs doing first, what needs nothing last.
//
// It exists because the published API description enumerated THREE of the four for the whole of
// Issue #1497 item 2's life — `operator_attested` was added to the derivation and to all three
// consumers, and the one sentence an external reader relies on to know the enumeration was left
// short. Go cannot enumerate a const block reflectively, so this slice is the single place a fifth
// state has to be added, and the tests that range over it are what make forgetting it loud.
//
// ⚠ It is NOT a substitute for exhaustively handling each state where the states MEAN different
// things — SummarisePlaidOrphanedItems' switch and the admin surface's per-state rendering are
// deliberately explicit, because folding two of these together is the defect, not the omission.
var AllOrphanedItemAdminStates = []OrphanedItemAdminState{
	OrphanedItemManualInterventionRequired,
	OrphanedItemAutoRetrying,
	OrphanedItemOperatorAttested,
	OrphanedItemResolved,
}

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
	// OperatorResolvedAt / OperatorResolvedBy / OperatorResolutionNote are the OPERATOR ATTESTATION —
	// a human's unverified statement that they removed this Item in Plaid's dashboard themselves.
	// Issue #1497 item 2, migration 000137.
	//
	// ⚠ DELIBERATELY SEPARATE FROM ResolvedAt AND NEVER WRITTEN TOGETHER WITH IT. ResolvedAt is
	// written only by the automatic sweep, on evidence from Plaid, and its write purges the
	// credential in the same statement. These three are written only by an operator, on no evidence,
	// and their write touches neither the credential nor ResolvedAt. See
	// OrphanedItemOperatorAttested.
	//
	// OperatorResolvedBy is the ATTESTING STAFF MEMBER's Clerk user id, not the erased data subject's
	// — this table's no-user-id rule (migration 000134) is about the person whose data was erased,
	// and an accountability record naming the employee who acted is the opposite of a surviving
	// pointer to them.
	OperatorResolvedAt     *time.Time
	OperatorResolvedBy     string
	OperatorResolutionNote string
}

// OperatorHasAttested reports whether a human has claimed to have handled this Item at Plaid.
//
// A method rather than an inline `!= nil` at each site because three readers ask the question
// (AdminState, AdminView, and the admin write endpoint's first-wins check) and "attested" must mean
// the same thing in all three.
func (o PlaidOrphanedItem) OperatorHasAttested() bool {
	return o.OperatorResolvedAt != nil
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
//  2. An OPERATOR ATTESTATION (Issue #1497 item 2) wins over everything below it, and loses to (1).
//     See the two paragraphs beneath this list — the ordering on both sides of it is load-bearing.
//  3. A dead token (absent, purged, or past its deadline) means manual intervention. This is the state
//     the table exists to surface.
//  4. A token this process could not decrypt is already covered by (3) — the repository leaves
//     AccessToken empty and sets TokenUndecryptable, so TokenIsLive is false. This check is
//     narrower and not redundant with it: it catches the row whose LAST RETRY could not decrypt even
//     though the reader can, which is what a key rolled out to some processes and not others looks
//     like. See OrphanTokenUndecryptableCode.
//  5. A live token with no OBSERVED retry within OrphanedItemRetryStaleAfter means manual intervention:
//     the row must not claim an automated retry it has no evidence of. See that constant.
//  6. Only then: auto-retrying.
//
// ⚠ WHY THE ATTESTATION IS ABOVE (3), (4) AND (5), i.e. why adding a column was not enough. All three
// of those checks are ways of concluding "a human must go and do this at Plaid". An attested row is
// one where a human already says they did — so leaving the attestation below them would have the
// surface demanding, indefinitely, the exact work its own record says was performed. That is the
// "mark written, work never happens" defect run backwards, and it is what makes this a change to the
// DERIVATION and not merely to the schema.
//
// ⚠ WHY IT IS BELOW (1), AND THIS ORDER IS PINNED BY A TEST. Resolved is Plaid's own answer; attested
// is a sentence somebody typed. Proof beats claim, so a row that was attested and LATER proved
// removed reports "resolved" — the stronger fact. Reordering these two to "show the human's statement
// first, it is more recent" would silently downgrade verified removals to unverified claims;
// TestPlaidOrphanedItem_AdminState_ProvedRemovalBeatsAnOperatorAttestation fails if anyone does.
//
// RetryAttemptCount is deliberately absent from this derivation. Nothing in this system gives up on an
// attempt count, so a high count is information for the operator, never a state.
func (o PlaidOrphanedItem) AdminState(asOf time.Time) OrphanedItemAdminState {
	if o.ResolvedAt != nil {
		return OrphanedItemResolved
	}
	if o.OperatorHasAttested() {
		return OrphanedItemOperatorAttested
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
	// "operator_attested" (a colleague says they already removed it — unverified, see
	// OrphanedItemOperatorAttested), or "resolved" (proved removed; history).
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
	// ResolvedAt is when an automated retry PROVED the Item gone at Plaid. Null on every other row,
	// including an operator-attested one — the two are never both written by the same act.
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	// OperatorResolvedAt / OperatorResolvedBy / OperatorResolutionNote carry the operator attestation
	// so that a merely-CLAIMED resolution stays auditable rather than becoming an anonymous tick.
	// Issue #1497 item 2.
	//
	// ⚠ ALL THREE ARE EXPOSED, DELIBERATELY, AND THE NOTE IS ONE OF THEM. A claim with no attributable
	// author and no stated reason cannot be checked against Plaid later, which would leave this surface
	// unable to distinguish a diligent operator from a row somebody clicked to make a red badge go
	// away. That is the whole risk of accepting an unverified attestation, so the mitigation is that
	// the attestation is never anonymous. OperatorResolvedBy is the STAFF member's Clerk id (see the
	// domain field's comment) — it is not the erased user's, and this view still carries no user PII.
	OperatorResolvedAt     *time.Time `json:"operator_resolved_at,omitempty"`
	OperatorResolvedBy     string     `json:"operator_resolved_by,omitempty"`
	OperatorResolutionNote string     `json:"operator_resolution_note,omitempty"`
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
		OperatorResolvedAt:        o.OperatorResolvedAt,
		OperatorResolvedBy:        o.OperatorResolvedBy,
		OperatorResolutionNote:    o.OperatorResolutionNote,
	}
}

// PlaidOrphanedItemHealthScanCap bounds how many plaid_orphaned_items rows the /admin/health state
// derivation (SystemHealthService) and the periodic monitoring pass (RetentionCleanupCheckWorker)
// will load in one pass. Issue #1497 items 4 and 5.
//
// WHY A CAP AT ALL, GIVEN THIS DERIVATION IS SUPPOSED TO BE BOUNDED BY THE TABLE'S OWN SIZE.
// Migrations 000134 and 000135 both argue this table holds SINGLE DIGITS OF ROWS, and that argument
// is why the state counts are derived in Go over a full read instead of as a SQL COUNT ... FILTER.
// The argument is sound about the RATE rows arrive at — it takes a permanently-unremovable Plaid
// Item AND a completed erasure to write one, and the C2 guard makes the erasure wait out a 7-day
// terminal-failure window first — but it is NOT a bound. Nothing prunes this table: there is no
// DELETE FROM plaid_orphaned_items anywhere in production code, and MarkResolved only STAMPS
// resolved_at, so resolved rows accumulate as history forever. The population is therefore
// monotonically non-decreasing in the number of stuck-Item erasures over the product's lifetime.
//
// So the cap is the honest encoding of the size argument rather than a restatement of it: at single
// digits it can never be reached, and IF IT IS REACHED the readers refuse to report a number at all
// (Available stays false, and an Error is logged naming this constant) instead of silently reporting
// a partial scan as a total. That refusal is the signal migration 000134 asked for in prose — "revisit
// if it ever grows, which would itself be the alarming signal" — made mechanical. The remedy at that
// point is server-side totals by state, NOT a bigger number here.
//
// ⚠ THE CAP IS NOT THE SEVEN-ROW POPULATION IT GUARDS. 500 leaves four orders of magnitude of
// headroom over "single digits", so a legitimately growing-but-healthy table does not trip it on a
// slow news day; it is a tripwire, not a routine limit.
const PlaidOrphanedItemHealthScanCap = 500

// SummarisePlaidOrphanedItems derives the aggregate operator picture of plaid_orphaned_items: how
// many rows are in each AdminState, plus the worst unresolved retry_attempt_count. Issue #1497
// items 4 and 5.
//
// ⚠ WHY THIS IS A GO DERIVATION AND NOT A SQL `COUNT(*) FILTER (WHERE ...)`, stated precisely
// because an earlier version of this justification overstated it and the overstatement was caught.
// Of AdminState's checks, exactly ONE resists SQL: the TokenIsLive check, which depends on
// whether THIS PROCESS, RIGHT NOW, can decrypt the stored ciphertext — the repository reports an
// undecryptable token as AccessToken == "", a fact no predicate over the table can see because the
// key lives in the application. The resolved_at check, the operator-attestation check (Issue #1497
// item 2 — a plain operator_resolved_at IS NOT NULL), the last_retry_error_code check (that column is
// plaintext) and the staleness check (a plain interval comparison over last_retry_at/orphaned_at) are
// ALL SQL-expressible. So the accurate statement is "one of the checks forces a Go derivation",
// not "no COUNT can reproduce this state" — the conclusion survives, the reasoning is narrower than
// it was first written, and adding a fifth SQL-expressible check did not change it either way.
//
// ⚠ THE STATE COUNTS ARE NOT ONE "UNRESOLVED" TOTAL, and collapsing them would defeat the
// feature. auto_retrying means DO NOTHING — acting at Plaid now duplicates work in flight — while
// manual_intervention_required means GO REMOVE THIS ITEM IN PLAID'S DASHBOARD OR IT MAY KEEP
// BILLING. One number covering both tells an operator neither thing. The same argument keeps
// operator_attested out of Resolved: "a colleague says they did it" and "Plaid confirmed it is gone"
// are different degrees of certainty about the same outstanding vendor obligation.
//
// asOf is taken rather than read from the clock, for the reason #1466 threaded it through every
// liveness predicate: two of AdminState's checks are time-dependent, so a derivation that read the
// clock internally could not be tested at the boundaries it exists to enforce.
//
// Available is deliberately LEFT AT ITS ZERO VALUE (false) here. This function cannot know whether
// the slice it was handed is a complete read or the truncated remains of a failed one, so marking a
// summary trustworthy is the CALLER's act, made explicitly, once, on the success path — the
// fail-closed direction: any future path that forgets to say "this is a real reading" reports
// "unknown", never a falsely confident zero. See PlaidOrphanedItemMetrics.Available.
func SummarisePlaidOrphanedItems(items []PlaidOrphanedItem, asOf time.Time) PlaidOrphanedItemMetrics {
	var m PlaidOrphanedItemMetrics
	for _, item := range items {
		switch item.AdminState(asOf) {
		case OrphanedItemAutoRetrying:
			m.AutoRetrying++
		case OrphanedItemManualInterventionRequired:
			m.ManualInterventionRequired++
		case OrphanedItemResolved:
			m.Resolved++
		case OrphanedItemOperatorAttested:
			// ⚠ NOT FOLDED INTO Resolved. Both mean "no further manual work is expected", which is
			// exactly the tempting simplification, and it would destroy the only distinction #1497
			// item 2 exists to record: one of these two numbers is what Plaid told us, the other is
			// what a colleague told us. See OrphanedItemOperatorAttested.
			m.OperatorAttested++
		}
		// UNRESOLVED ONLY. A resolved row's attempt history is finished, so letting it drive this
		// gauge would latch an alert forever on work that actually succeeded — the "mark outlives the
		// work" shape this branch has closed five times already. Read straight off ResolvedAt rather
		// than off the AdminState above, because a row can be unresolved AND manual-intervention, and
		// a row being retried into the ground is exactly the case this gauge exists for.
		//
		// ⚠ AN OPERATOR-ATTESTED ROW STILL COUNTS HERE, and that is deliberate rather than an
		// oversight of the #1497-item-2 change: ResolvedAt is the only thing exempted, because it is
		// the only thing that PROVES the retrying has stopped mattering. An attestation does not stop
		// the sweep (see plaidOrphanedItemRepository.GetRetryablePage's decision), so the count keeps
		// climbing — and a row still failing 20-odd removals after somebody attested it is exactly the
		// case worth telling a human about, since either the claim was wrong or the Item really is
		// still at Plaid. Exempting attested rows here would let an unverified sentence silence the
		// one gauge capable of exposing it as false.
		if item.ResolvedAt == nil && int64(item.RetryAttemptCount) > m.MaxUnresolvedRetryAttemptCount {
			m.MaxUnresolvedRetryAttemptCount = int64(item.RetryAttemptCount)
		}
	}
	return m
}

// SummarisePlaidOrphanedItemsBounded is SummarisePlaidOrphanedItems plus the cap check, so the two
// readers of this state — SystemHealthService.getPlaidOrphanedItemMetrics (the /admin/health JSON)
// and RetentionCleanupCheckWorker.monitorOrphanedPlaidItems (the Prometheus gauges) — share ONE
// definition of both the derivation and its bound. Issue #1497 items 4 and 5.
//
// It takes `total` (the repository's own COUNT, alongside the page it returned) rather than deriving
// the population size from len(items), because len(items) cannot distinguish "the whole table" from
// "the first PlaidOrphanedItemHealthScanCap rows of a larger one" — which is precisely the case that
// must be refused. Past the cap this returns an ERROR and no counts at all: a count over the first
// 500 of N rows would UNDERSTATE the manual-intervention population, the one direction this state
// must never err in, and it would look like an answer. See PlaidOrphanedItemHealthScanCap.
//
// ⚠ `total` MUST COME FROM THE SAME SNAPSHOT AS `items`, which is why plaidOrphanedItemRepository.List
// derives both from a single statement (`COUNT(*) OVER ()`) rather than from a COUNT followed by a
// page. Two non-transactional statements can hand this function a total of exactly the cap alongside
// a page truncated out of a larger table, and the refusal below — the entire purpose of the cap —
// would not fire. See List's comment.
//
// The empty-page guard is the second half of that: a page of zero rows drawn from a table that is
// not empty is not a reading of anything, and summarising it would publish confident zeroes for a
// population that may be entirely manual-intervention work. It is unreachable from either metrics
// reader (both read offset 0), and it is here precisely so that staying unreachable is not what the
// correctness depends on.
//
// The cap boundary is INCLUSIVE — exactly cap rows is a complete read and is reported. See
// TestPlaidOrphanedItemHealthScanCap_IsPinnedAndGovernsRefusalPastIt.
//
// Available is left false on both paths for the reason SummarisePlaidOrphanedItems leaves it false:
// claiming a reading is trustworthy is the caller's explicit act.
func SummarisePlaidOrphanedItemsBounded(items []PlaidOrphanedItem, total int, asOf time.Time) (PlaidOrphanedItemMetrics, error) {
	if total > PlaidOrphanedItemHealthScanCap {
		return PlaidOrphanedItemMetrics{}, fmt.Errorf(
			"plaid_orphaned_items holds %d rows, past the %d-row cap this in-Go state derivation is bounded by; migrations 000134/000135 argued this table holds single digits of rows and it no longer does, so these counts need server-side totals by state (COUNT ... FILTER) rather than a bigger cap",
			total, PlaidOrphanedItemHealthScanCap)
	}
	if total > 0 && len(items) == 0 {
		return PlaidOrphanedItemMetrics{}, fmt.Errorf(
			"plaid_orphaned_items holds %d rows but the page handed to this derivation is empty, so there is nothing here to summarise; a count of zero over a non-empty table would understate the manual-intervention population, the one direction this state must never err in",
			total)
	}
	return SummarisePlaidOrphanedItems(items, asOf), nil
}
