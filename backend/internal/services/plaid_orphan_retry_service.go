package services

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

// orphanRetryTransportErrorCode is the LOCAL sentinel recorded when /item/remove failed with something
// that carries no Plaid error_code at all — a DNS failure, a reset connection, a context deadline.
// Issue #1497.
//
// It exists because the alternative is writing "" into last_retry_error_code, and "" already means
// something else in that column: "no retry has ever failed". Recording an attempt that DID fail as one
// that never happened is the kind of quiet lie that makes an operator trust a row they should not.
// Spelled outside Plaid's vocabulary, like NO_USABLE_ACCESS_TOKEN and ORPHAN_TOKEN_UNDECRYPTABLE.
const orphanRetryTransportErrorCode = "ORPHAN_RETRY_TRANSPORT_ERROR"

// orphanRetryRepo is the narrow slice of PlaidOrphanedItemRepository this sweep needs.
//
// ⚠ PurgeExpiredTokens IS DELIBERATELY ABSENT. This service must not be able to purge, because a purge
// reachable only through the Plaid-dependent retry path is a purge that does not happen when Plaid is
// unconfigured, when this service is nil, or when its job is unregistered — which is precisely the
// failure the separate purge sweep exists to make impossible. Keeping it out of this interface makes
// that a compile-time property rather than a convention.
type orphanRetryRepo interface {
	GetRetryablePage(ctx context.Context, asOf time.Time, after *repository.OrphanRetryCursor, limit int) ([]domain.PlaidOrphanedItem, error)
	MarkResolved(ctx context.Context, id string, asOf time.Time) (bool, error)
	RecordRetryFailure(ctx context.Context, id, errorCode string, asOf time.Time) error
}

// OrphanRetrySweepResult is one run's tally, returned so the worker can log it and so tests can assert
// which of several mutually exclusive dispositions each row got.
type OrphanRetrySweepResult struct {
	// Attempted counts rows the sweep actually took a decision about, including skipped ones.
	Attempted int
	// Resolved counts Items proved gone from Plaid (a successful /item/remove, or ITEM_NOT_FOUND).
	Resolved int
	// Failed counts rows whose /item/remove failed recoverably and will be retried next run.
	Failed int
	// Undecryptable counts rows whose retained ciphertext could not be decrypted.
	Undecryptable int
	// Skipped counts rows the repository returned with no usable credential at all — defensive, since
	// the SQL predicate excludes them.
	Skipped int
	// RecordErrors counts rows whose OUTCOME could not be written back. Non-zero means the attempt
	// happened and the record does not know it, which is worth an operator's attention.
	RecordErrors int
	// DeadlineElapsed counts rows that were live when the page was read but whose token_expires_at had
	// passed by the time their own turn came. Distinct from Skipped (which means "no credential at all")
	// and from Failed (no Plaid call was made), because it is the only counter that says a retention
	// deadline was actually reached mid-run — the signal that a sweep is running long enough to matter.
	DeadlineElapsed int
	// CapHit reports that the per-sweep cap stopped this run early and a remainder was deferred.
	CapHit bool
}

// OrphanRetryResume is the persisted cross-run resume position for this sweep, supplied per RUN by
// the caller. Issue #1477.
//
// ⚠ IT IS DECLARED HERE AND IMPLEMENTED ELSEWHERE, DELIBERATELY. The mechanism — the sweep_cursors
// row, the load, the wrap decision, the save/clear asymmetry, repository.NextPosition — is the one
// the other three capped sweeps already share (queue/jobs/sweep_resume.go, designed in
// repository/sweep_cursor_repository.go). This interface exists so that the one paging loop that
// lives in the service layer can USE that mechanism rather than grow a second one; there is no
// implementation of it in this package and there must not be.
//
// nil is legal and means "do not persist anything": the sweep then starts from the oldest candidate
// every run, which is exactly the pre-#1477 behaviour. That keeps this package's unit tests free of
// a database, and it is why production wiring (main.go → PlaidOrphanRetryWorkerDeps.SweepCursors)
// is the thing to check if the starvation below ever reappears.
type OrphanRetryResume interface {
	// StartCursor is the position to resume from, or nil to start from the oldest candidate.
	StartCursor() *repository.OrphanRetryCursor
	// WrapNow reports whether an empty page at a RESUMED position means the stored watermark is
	// past the end (true — it has been cleared, and the caller must re-query from nil within this
	// same run) rather than "there is nothing to do".
	WrapNow(ctx context.Context, processed int) bool
	// Finish persists where a capped run stopped, or clears the position when the run reached the
	// end of the candidate set.
	Finish(ctx context.Context, cappedStop bool, reached repository.SweepPosition)
}

// PlaidOrphanRetryService re-attempts Plaid /item/remove for records in plaid_orphaned_items that still
// hold a live, unexpired credential. Issue #1497.
//
// WHY THIS IS WORTH DOING AT ALL, since migration 000134 argued it was not: the population reaching
// that table is NOT only items whose token Plaid has already rejected. DeleteUserCascade copies every
// row with `plaid_removed_at IS NULL`, which includes 'replaced' re-link leftovers whose /item/remove
// was NEVER ATTEMPTED — the re-link path does not call it. For those rows this sweep is the only
// automated path that has ever existed, and each one is a Plaid Item that may still be billing.
//
// WHAT IT DELIBERATELY CANNOT DO: purge. See orphanRetryRepo.
type PlaidOrphanRetryService interface {
	// Sweep processes up to maxAttempts records in pages of pageSize and reports the tally.
	//
	// asOf is the SELECTION instant: it fixes the keyset page and stamps the outcome writes. It is NOT
	// the instant the retention deadline is judged at — that is re-read from the wall clock immediately
	// before each credential is presented to Plaid, because a run can outlive a deadline it started
	// inside. See retryOne.
	//
	// resume carries the cross-run position (Issue #1477) and may be nil — see OrphanRetryResume.
	Sweep(ctx context.Context, asOf time.Time, pageSize, maxAttempts int, resume OrphanRetryResume) (OrphanRetrySweepResult, error)
}

type plaidOrphanRetryService struct {
	repo         orphanRetryRepo
	plaidService plaidServiceForCleanup
	logger       *slog.Logger
	// now is READ AGAIN FOR EVERY ROW, and it is deliberately NOT the sweep's asOf. asOf is the
	// selection instant and must stay frozen for the keyset page to be stable; the retention deadline,
	// by contrast, has to be judged against the clock as it stands when the credential is used. A
	// function rather than a value because "read the clock again" is untestable otherwise.
	now func() time.Time
}

// NewPlaidOrphanRetryService builds the sweep. plaidService may be nil — that is the "Plaid not
// configured" deployment — and Sweep then refuses with an error rather than panicking or, worse,
// reporting a successful run in which nothing was retried.
func NewPlaidOrphanRetryService(repo orphanRetryRepo, plaidService plaidServiceForCleanup) *plaidOrphanRetryService { //nolint:revive // deliberate: unexported return mirrors this package's other constructors whose tests reach for the concrete type
	return &plaidOrphanRetryService{
		repo:         repo,
		plaidService: plaidService,
		logger:       slog.Default(),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the wall clock the per-row retention deadline is judged against. Tests only: a
// sweep of up to 1,000 sequential Plaid round-trips cannot otherwise be made to cross a deadline
// without sleeping through one. A nil clock is ignored, matching SetLogger.
func (s *plaidOrphanRetryService) SetClock(clock func() time.Time) {
	if clock != nil {
		s.now = clock
	}
}

// SetLogger overrides the default logger. Setter rather than a constructor parameter to match this
// package's existing convention (SetAnalytics, SetWorkClaimsRepo).
func (s *plaidOrphanRetryService) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// Sweep walks the retryable records in keyset order and re-attempts each one's removal.
//
// PER-ROW FAILURES ARE RECORDED AND STEPPED OVER; A PAGE-QUERY FAILURE ABORTS. The asymmetry is the
// same one item_cleanup_check.go draws: one bad Item must not stop the others, but a sweep that cannot
// read its work list has not done its work and must say so, because River retrying it is the recovery.
//
// BOUNDED TWICE, like every other sweep on this branch: pageSize bounds one query's result set,
// maxAttempts bounds the total decisions one run makes. Hitting the cap logs a WARN and defers the
// remainder to the next run — nothing is dropped, because nothing about a deferred row changes.
//
// THE CURSOR ADVANCES PAST ROWS THAT FAILED, not merely past rows that succeeded. That is the whole
// reason it is a keyset cursor rather than a re-run of the same predicate: a row whose removal keeps
// failing (or whose outcome cannot be written back) must not occupy the oldest slot of every subsequent
// page within this run and starve everything behind it.
//
// ⚠ AND ACROSS RUNS IT IS THE SAME PROBLEM, which this comment used to describe only in its benign
// half. A permanently-failing row IS re-attempted every run — correct, since only the retention
// deadline ends the retrying — but it also STAYS at the head of the candidate set while doing so:
// GetRetryablePage's predicate is (unresolved, credential present, deadline not passed) with no
// attempt ceiling and no backoff on last_retry_at, and RecordRetryFailure moves none of those. So
// before Issue #1477, when the cursor restarted at nil on every run, a backlog larger than
// maxAttempts whose head kept failing meant every run re-spent its whole budget on the same oldest
// records and NOTHING past the cap was ever retried — until those records' own credentials expired,
// after which no automated path can ever remove their Items. That is a structural starvation, not
// slow drainage, and unlike item_cleanup's it needs no database failure: an ordinary Plaid failure,
// which is the case this sweep exists for, is enough.
//
// Since #1477 the position is PERSISTED (resume, see OrphanRetryResume) and the next run continues
// from where this one stopped. The replacement guarantee is bounded, not absolute — it is stated
// once, with its bound, in repository/sweep_cursor_repository.go's header. Do not restate it here as
// "no starvation".
func (s *plaidOrphanRetryService) Sweep(ctx context.Context, asOf time.Time, pageSize, maxAttempts int, resume OrphanRetryResume) (OrphanRetrySweepResult, error) {
	var res OrphanRetrySweepResult

	if s.plaidService == nil {
		// Not a panic and not a silent success: a run that could not retry anything must be
		// distinguishable from one that found nothing to retry. The purge sweep is deliberately
		// independent of this service, so credentials are still bounded while this is true.
		return res, errors.New("plaid orphan retry sweep: no Plaid client configured; nothing was retried")
	}
	if pageSize <= 0 || maxAttempts <= 0 {
		return res, fmt.Errorf("plaid orphan retry sweep: pageSize and maxAttempts must be positive, got %d/%d", pageSize, maxAttempts)
	}

	// Issue #1477: resume where the last CAPPED run stopped. A nil resume is the pre-#1477
	// behaviour — start from the oldest candidate — and is what every unit test in this package
	// passes; production wires it in main.go.
	var cursor *repository.OrphanRetryCursor
	if resume != nil {
		cursor = resume.StartCursor()
	}
	// reached records only the TIMESTAMP half of the last position this run visited — never the row
	// id. See repository/sweep_cursor_repository.go's header for why sweep_cursors stores no row
	// identity.
	var reached repository.SweepPosition

	for {
		remaining := maxAttempts - res.Attempted
		if remaining <= 0 {
			res.CapHit = true
			s.logger.Warn("plaid orphan retry sweep hit its per-run cap; the remainder is deferred to the next run",
				"attempted", res.Attempted, "cap", maxAttempts)
			break
		}
		limit := pageSize
		if remaining < limit {
			limit = remaining
		}

		page, err := s.repo.GetRetryablePage(ctx, asOf, cursor, limit)
		if err != nil {
			return res, fmt.Errorf("plaid orphan retry sweep: read retryable page: %w", err)
		}
		if len(page) == 0 {
			// An empty page at a RESUMED position means the persisted cursor is past the end, not
			// that there is no work: clear it and re-query from the beginning within this same run.
			if resume != nil && resume.WrapNow(ctx, res.Attempted) {
				cursor = nil
				continue
			}
			break
		}
		// Truncate defensively rather than trusting the query's LIMIT, so the cap is a property of THIS
		// loop and not of a repository that happens to honour it — the same reason
		// ItemCleanupCheckWorker.Work truncates its page before processing it. Without this the cap
		// becomes "cap, rounded up to a page", which is not what "at most maxAttempts" means.
		if len(page) > limit {
			page = page[:limit]
		}

		for _, row := range page {
			res.Attempted++
			s.retryOne(ctx, row, asOf, &res)
		}

		last := page[len(page)-1]
		cursor = &repository.OrphanRetryCursor{OrphanedAt: last.OrphanedAt, ID: last.ID}
		lastOrphanedAt := last.OrphanedAt
		reached = repository.SweepPosition{TS1: &lastOrphanedAt}

		if len(page) < limit {
			break
		}
	}

	// Persist the remainder position (cap hit) or clear it (end of set reached) — Issue #1477.
	// res.CapHit is set by exactly one branch above, the one that stops the run early, so it is
	// precisely "this run stopped with a remainder".
	if resume != nil {
		resume.Finish(ctx, res.CapHit, reached)
	}

	return res, nil
}

// retryOne decides and records the disposition of a single record. It never returns an error: every
// outcome, including "we could not write the outcome down", is a counter on res so the run's log line
// is a complete account of what happened.
func (s *plaidOrphanRetryService) retryOne(ctx context.Context, row domain.PlaidOrphanedItem, asOf time.Time, res *OrphanRetrySweepResult) {
	// The rotated-key case, and it is checked BEFORE the empty-token check because it is a different
	// fact with a different remedy: the credential is present and unusable, rather than absent. Plaid is
	// deliberately not called — presenting "" would produce a misleading "access token cannot be empty"
	// error attributed to Plaid — and the sentinel is recorded so the record stops reading as
	// auto-retrying (domain.PlaidOrphanedItem.AdminState). The token is NOT purged here; see
	// domain.OrphanTokenUndecryptableCode for why a missing key is treated as recoverable.
	if row.TokenUndecryptable {
		res.Undecryptable++
		s.logger.Error("retained Plaid credential for an orphaned Item could not be decrypted; no automated retry can succeed until the key is restored, and the record now asks for manual intervention",
			"orphan_record_id", row.ID, "plaid_item_id", row.PlaidItemID)
		s.record(ctx, row, domain.OrphanTokenUndecryptableCode, asOf, res)
		return
	}
	if row.AccessToken == "" {
		// Defensive: GetRetryablePage's predicate requires a non-NULL ciphertext, so reaching here means
		// a stored value that decrypted to the empty string. There is nothing to send.
		res.Skipped++
		s.logger.Warn("orphaned Item record holds no usable credential; skipping",
			"orphan_record_id", row.ID, "plaid_item_id", row.PlaidItemID)
		return
	}

	// THE RETENTION DEADLINE IS RE-JUDGED HERE, AGAINST THE CLOCK AS IT STANDS NOW — not against asOf.
	//
	// GetRetryablePage already applied `token_expires_at > asOf`, but asOf is the instant the PAGE was
	// selected, and one run may make up to maxOrphanRetriesPerSweep (1,000) sequential Plaid round-trips
	// after that instant. So a row that was live at selection can be minutes past its deadline by the
	// time its own turn arrives, and re-testing it against the same frozen asOf would not notice: it is
	// the identical comparison that already passed. Migration 000135 and domain.TokenIsLive promise a
	// HARD deadline on the credential's USE ("NEVER read this column without checking
	// token_expires_at"), and the entire GDPR defensibility of retaining an erased user's bank
	// credential rests on that promise binding the use rather than the sweep's start.
	//
	// THE ROW IS NOT RE-READ, deliberately. token_expires_at is written exactly once, by
	// DeleteUserCascade's INSERT (profile_repository.go), and no statement anywhere updates it — so the
	// snapshot's deadline is already the authoritative value and only the clock has moved. A re-read
	// would cost a query per row, widen the deliberately-narrow orphanRetryRepo interface, and STILL not
	// close this window (the row could expire between the re-read and the call). Advancing the clock is
	// the only thing that closes it. A concurrent purge NULLing the ciphertext is bounded by the same
	// deadline this check enforces, and a duplicate in-flight retry is already bounded to one wasted
	// ITEM_NOT_FOUND by MarkResolved's first-wins.
	//
	// SKIPPED, NOT FAILED: no call was made, so RecordRetryFailure would attribute an error code to an
	// attempt that never happened. The row needs no write — the purge sweep owns the row from here, and
	// its predicate is this same elapsed deadline.
	if useAt := s.now(); !row.TokenIsLive(useAt) {
		res.DeadlineElapsed++
		s.logger.Warn("an orphaned Item's retained credential reached its retention deadline mid-sweep; it was NOT presented to Plaid and the Item stays outstanding for manual intervention",
			"orphan_record_id", row.ID, "plaid_item_id", row.PlaidItemID,
			"token_expires_at", row.TokenExpiresAt, "checked_at", useAt, "selected_at", asOf)
		return
	}

	err := s.plaidService.RemoveItem(ctx, row.AccessToken)
	if err == nil || isPlaidItemAlreadyGone(err) {
		// ITEM_NOT_FOUND is the same OUTCOME as a successful removal — Plaid has no such Item, so nothing
		// is billing for it. The identical rule already governs the pre-erasure path
		// (isPlaidItemAlreadyGone, Issue #1413 H2); recording it as a failure here would re-attempt an
		// Item Plaid has forgotten on every run until the deadline.
		updated, resolveErr := s.repo.MarkResolved(ctx, row.ID, asOf)
		if resolveErr != nil {
			res.RecordErrors++
			s.logger.Error("removed an orphaned Plaid Item but could not record the resolution; the retained credential is still on the record and will be purged at its deadline",
				"orphan_record_id", row.ID, "plaid_item_id", row.PlaidItemID, "error", resolveErr)
			return
		}
		res.Resolved++
		s.logger.Info("removed an orphaned Plaid Item after its owner's erasure; the retained credential is purged with the same statement",
			"orphan_record_id", row.ID, "plaid_item_id", row.PlaidItemID,
			"already_gone_at_plaid", err != nil, "first_resolution", updated)
		return
	}

	res.Failed++
	code := plaidErrorCodeOrTransport(err)
	s.logger.Warn("retrying removal of an orphaned Plaid Item failed; it stays outstanding until it succeeds or its credential expires",
		"orphan_record_id", row.ID, "plaid_item_id", row.PlaidItemID, "error_code", code, "error", err)
	s.record(ctx, row, code, asOf, res)
}

// record writes one failure outcome and counts the case where even that write failed.
func (s *plaidOrphanRetryService) record(ctx context.Context, row domain.PlaidOrphanedItem, code string, asOf time.Time, res *OrphanRetrySweepResult) {
	if err := s.repo.RecordRetryFailure(ctx, row.ID, code, asOf); err != nil {
		res.RecordErrors++
		s.logger.Error("could not record the outcome of an orphaned-Item retry; the attempt happened and the record does not know it",
			"orphan_record_id", row.ID, "error_code", code, "error", err)
	}
}

// plaidErrorCodeOrTransport extracts Plaid's error_code, falling back to a local sentinel rather than
// to the empty string — see orphanRetryTransportErrorCode for why "" is not an acceptable value here.
//
// Spelled out rather than reusing domain.ClassifyPlaidError because that function's job is to decide
// retryability for the SYNC path and it maps several codes onto its own taxonomy; what is wanted here
// is the code itself, verbatim, for an operator to read.
func plaidErrorCodeOrTransport(err error) string {
	var apiErr plaid.GenericOpenAPIError
	if errors.As(err, &apiErr) {
		if plaidErr, convErr := plaid.ToPlaidError(apiErr); convErr == nil && plaidErr.ErrorCode != "" {
			return plaidErr.ErrorCode
		}
	}
	return orphanRetryTransportErrorCode
}
