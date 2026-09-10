package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// UserAccountService provides business logic for managing user accounts.
// UserAccount is a stable abstraction for bank accounts that survives Plaid Item re-links.
// When a user disconnects and reconnects a bank, the UserAccount persists and preserves
// user customizations (nickname, color, etc.) while the underlying Plaid account_id changes.
//
// Issue #171 - Plaid Re-Link Handling
type UserAccountService interface {
	// MatchOrCreate finds an existing UserAccount or creates a new one.
	// This is the core re-link matching logic: match on (user_id, institution_id, mask,
	// identity_type, identity_subtype) — #1569 round 11 task 4 narrowed this from
	// (user_id, institution_id, mask) alone.
	// If found, updates type/subtype if different and returns existing (preserving customizations).
	// If not found, creates new UserAccount with provided details.
	//
	// The post-match type/subtype and un-hide writes are scoped to userID as the authenticated
	// actor, not to the owner recorded on the matched row, so they cannot cross tenants (#1566).
	// Returns repository.ErrUserAccountNotFound (wrapped) if userID does not own the matched row.
	// Other failures of those two writes are non-fatal: the matched account is still returned.
	MatchOrCreate(ctx context.Context, userID, institutionID string, mask *string, accountName, accountType, accountSubtype string) (*domain.UserAccount, error)

	// GetByUserID retrieves all user accounts for a user.
	// If includeHidden is false, filters out accounts where is_hidden=true.
	GetByUserID(ctx context.Context, userID string, includeHidden bool) ([]domain.UserAccount, error)

	// GetByID retrieves a user account by ID with ownership validation.
	GetByID(ctx context.Context, id, userID string) (*domain.UserAccount, error)

	// UpdateCustomization updates user account customization fields.
	// Returns the updated UserAccount.
	UpdateCustomization(ctx context.Context, id, userID string, req *domain.UpdateUserAccountRequest) (*domain.UserAccount, error)

	// SetHidden updates the is_hidden flag on a UserAccount, scoped to the authenticated actor.
	// Used by DeleteAccount to hide the UserAccount when its last active account is deleted.
	//
	// ⚠ hidden = true means a SYSTEM hide (hidden_source = 'system'), never a user-initiated one
	// — see the implementation. A user hide arrives through UpdateCustomization instead.
	// Returns repository.ErrUserAccountNotFound (unwrapped) if userID does not own userAccountID (#1566).
	SetHidden(ctx context.Context, userAccountID, userID string, hidden bool) error

	// SetInvestmentBackfillEnqueuer wires the PROMPTNESS half of the #1631 M4 backfill: when the
	// owner re-includes a previously excluded chain, this lets UpdateCustomization nudge an
	// investment-transactions sync for the item that carries the chain, instead of waiting for
	// Plaid's next INVESTMENTS_TRANSACTIONS webhook — which for a quiet brokerage can be months.
	//
	// ⚠ It is a NUDGE, NOT THE OBLIGATION. The obligation is
	// user_accounts.investment_backfill_requested_at, written in the same UPDATE that clears the
	// flag; whichever sync runs next honours it whether or not this enqueue ever happened. That is
	// why setter injection and a nil-safe no-op are acceptable here and would NOT be acceptable for
	// the obligation itself: the worst case of losing this is latency, and the worst case of losing
	// the obligation is the user's history never coming back.
	SetInvestmentBackfillEnqueuer(jobQueue queue.JobQueue, accountRepo repository.AccountRepository, logger *slog.Logger)
}

// userAccountService implements UserAccountService
type userAccountService struct {
	repo repository.UserAccountRepository

	// Optional, all three set together by SetInvestmentBackfillEnqueuer and all three nil-checked
	// as one. See that method for why losing them is only a latency cost.
	jobQueue    queue.JobQueue
	accountRepo repository.AccountRepository
	logger      *slog.Logger
}

// SetInvestmentBackfillEnqueuer wires the optional backfill nudge. See the interface method.
func (s *userAccountService) SetInvestmentBackfillEnqueuer(
	jobQueue queue.JobQueue,
	accountRepo repository.AccountRepository,
	logger *slog.Logger,
) {
	s.jobQueue = jobQueue
	s.accountRepo = accountRepo
	if logger == nil {
		logger = slog.Default()
	}
	s.logger = logger
}

// NewUserAccountService creates a new UserAccountService instance
func NewUserAccountService(repo repository.UserAccountRepository) UserAccountService {
	return &userAccountService{
		repo: repo,
	}
}

// MatchOrCreate finds an existing UserAccount or creates a new one.
// This is the core re-link matching logic.
func (s *userAccountService) MatchOrCreate(
	ctx context.Context,
	userID, institutionID string,
	mask *string,
	accountName, accountType, accountSubtype string,
) (*domain.UserAccount, error) {
	// Step 1: Try to find existing user account by (user_id, institution_id, mask, identity_type,
	// identity_subtype). #1569 round 11 task 4: identity is now PART of the match key, so a
	// brokerage and an IRA at one institution (same institution, both maskless) are no longer
	// matched onto the same chain.
	identityType := strPtrIfNotEmpty(accountType)
	identitySubtype := strPtrIfNotEmpty(accountSubtype)
	existing, err := s.repo.FindByInstitutionAndMask(ctx, userID, institutionID, mask, identityType, identitySubtype)
	if err != nil {
		return nil, fmt.Errorf("failed to find existing user account: %w", err)
	}

	if existing != nil {
		// Found existing - this is a re-link scenario
		return s.normalizeExistingUserAccount(ctx, userID, existing, accountType, accountSubtype)
	}

	// Step 2: No match found - create new UserAccount
	// #1569 round 11 task 3: IdentityType/IdentitySubtype are the FROZEN identity of this chain,
	// set here from the founding account's own type/subtype and never touched again — no
	// repository method updates them. PrimaryType/PrimarySubtype above are a SEPARATE,
	// last-writer-wins pair that DOES get overwritten on every re-link
	// (normalizeExistingUserAccount -> UpdatePrimaryTypeAndSubtype); do not conflate them.
	// #1569 round 11 task 4: IdentityType/IdentitySubtype ARE now read for matching — they are
	// PART of FindByInstitutionAndMask's key (passed as identityType/identitySubtype above), so
	// the "future task" this comment used to defer to has already landed.
	newUA := &domain.UserAccount{
		UserID:          userID,
		InstitutionID:   institutionID,
		Mask:            mask,
		AccountNameHint: strPtrIfNotEmpty(accountName),
		PrimaryType:     strPtrIfNotEmpty(accountType),
		PrimarySubtype:  strPtrIfNotEmpty(accountSubtype),
		IdentityType:    strPtrIfNotEmpty(accountType),
		IdentitySubtype: strPtrIfNotEmpty(accountSubtype),
		IsHidden:        false, // Default to visible
	}

	err = s.repo.Create(ctx, newUA)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateUserAccount) {
			// Lost a concurrent create race — re-find the winner's row and normalize it.
			winner, findErr := s.repo.FindByInstitutionAndMask(ctx, userID, institutionID, mask, identityType, identitySubtype)
			if findErr != nil {
				return nil, fmt.Errorf("failed to re-find user account after duplicate: %w", findErr)
			}
			if winner == nil {
				// Should not happen: Create said duplicate exists but FindBy found nothing
				return nil, fmt.Errorf("user account not found after duplicate create error")
			}
			return s.normalizeExistingUserAccount(ctx, userID, winner, accountType, accountSubtype)
		}
		return nil, err
	}

	return newUA, nil
}

// normalizeExistingUserAccount applies post-find updates to an existing UserAccount:
// updates type/subtype if they differ from the provided values, and un-hides the account
// if it was previously hidden (e.g. after a re-link).
//
// actorUserID is the AUTHENTICATED actor from MatchOrCreate — never existing.UserID. The
// repository scopes both writes with `WHERE id = $x AND user_id = $y` (#1566); passing the
// owner read off the matched row would make that predicate tautological, so it could never
// reject and a row belonging to somebody else would be silently rewritten.
//
// Error handling: an ownership rejection (repository.ErrUserAccountNotFound) is a tenancy
// violation, not a transient hiccup, so it is propagated to the caller. Any other error is
// non-fatal — the caller only needs the matched row, and its customizations are preserved
// either way. userAccountService has no logger dependency, so propagating is the only way
// to make a rejection observable (adding a logger would change the constructor and every
// wiring site; deliberately out of scope here).
//
// ⚠ WHAT PROPAGATING COSTS, recorded in #1537 round 9 because the paragraph above states the benefit
// and not the price. The tenancy predicate cannot actually reject on THIS path: `existing` comes from
// the user-scoped FindByInstitutionAndMask, so actorUserID always equals existing.UserID and the
// predicate is satisfied by construction. What the propagated error really reports is
// RowsAffected == 0 — the row vanished between the match and the update (a users-cascade racing a
// re-link). And the caller's response to any error from MatchOrCreate is to log and continue WITHOUT a
// user_account_id (account_service.go, "Continue without user_account_id"), which inserts an account
// with a NULL chain key — losing the stable re-link key #171/#1109 exist for. Before #1566 the same
// race left a stale type LABEL instead, which is strictly less damaging.
//
// So the trade is: a genuine cross-tenant write (unreachable here today, but reachable if a future
// caller resolves the id some other way) is now refused, at the cost of a lost chain key on a rare
// race. That is the right trade — a wrong-tenant write is unbounded, a NULL chain key is recoverable
// on the next sync — but it is a trade, and the earlier version of this comment presented it as pure
// gain.
func (s *userAccountService) normalizeExistingUserAccount(
	ctx context.Context,
	actorUserID string,
	existing *domain.UserAccount,
	accountType, accountSubtype string,
) (*domain.UserAccount, error) {
	// Update type/subtype if changed (new Plaid data takes precedence).
	existingType := derefPtr(existing.PrimaryType)
	existingSubtype := derefPtr(existing.PrimarySubtype)
	if existingType != accountType || existingSubtype != accountSubtype {
		err := s.repo.UpdatePrimaryTypeAndSubtype(ctx, existing.ID, actorUserID, accountType, accountSubtype)
		if errors.Is(err, repository.ErrUserAccountNotFound) {
			return nil, fmt.Errorf("user %s does not own user account %s: %w", actorUserID, existing.ID, err)
		}
		// Other errors are non-fatal: the type labels stay stale, customizations are preserved.
	}

	// Un-hide if the SYSTEM is what hid it (re-link after the chain lost its last active
	// account). Only update in-memory state on success to keep DB and caller in sync.
	//
	// ⚠ THE CAUSE IS THE WHOLE POINT (#1609). This used to un-hide on every re-link, full stop,
	// which destroyed a hide the user had set deliberately: by the time a re-link asks, a
	// user-hidden chain and a system-hidden one both have zero active accounts, so current state
	// cannot tell them apart. hidden_source (migration 000185) records which it was.
	//
	// Only 'system' is lifted. 'user' is the user's own decision. 'unknown' is 000185's
	// conservative classification for rows it could not tell apart, and a NULL cause on a hidden
	// row is the same claim — nobody recorded why — so it is treated the same way. The asymmetry
	// is deliberate: un-hiding an account the user hid is a privacy regression they cannot even
	// detect, while leaving a system hide in place is an annoyance they can undo.
	if existing.IsHidden && derefPtr(existing.HiddenSource) == domain.HiddenSourceSystem {
		err := s.repo.ClearHidden(ctx, existing.ID, actorUserID)
		switch {
		case errors.Is(err, repository.ErrUserAccountNotFound):
			return nil, fmt.Errorf("user %s does not own user account %s: %w", actorUserID, existing.ID, err)
		case err != nil:
			// Non-fatal: the row stays hidden, and so does the in-memory copy.
		default:
			existing.IsHidden = false
			existing.HiddenSource = nil
		}
	}

	return existing, nil
}

// GetByUserID retrieves all user accounts for a user.
func (s *userAccountService) GetByUserID(ctx context.Context, userID string, includeHidden bool) ([]domain.UserAccount, error) {
	accounts, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if includeHidden {
		return accounts, nil
	}

	// Filter out hidden accounts
	visible := make([]domain.UserAccount, 0, len(accounts))
	for _, acct := range accounts {
		if !acct.IsHidden {
			visible = append(visible, acct)
		}
	}

	return visible, nil
}

// GetByID retrieves a user account by ID with ownership validation.
func (s *userAccountService) GetByID(ctx context.Context, id, userID string) (*domain.UserAccount, error) {
	return s.repo.GetByIDAndUser(ctx, id, userID)
}

// UpdateCustomization updates user account customization fields.
func (s *userAccountService) UpdateCustomization(
	ctx context.Context,
	id, userID string,
	req *domain.UpdateUserAccountRequest,
) (*domain.UserAccount, error) {
	// Step 1: Get existing account with ownership validation
	existing, err := s.repo.GetByIDAndUser(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	// The PRE-UPDATE value of the exclusion flag, captured before Step 2 overwrites it. Used only
	// to decide whether to nudge a sync in Step 5 — never to decide whether the obligation is owed,
	// which is settled in SQL. See the ExcludeInvestmentSync block below.
	wasExcludedFromInvestmentSync := existing.ExcludeInvestmentSync

	// Step 2: Apply updates from request (nil = no change)
	if req.Nickname != nil {
		if *req.Nickname == "" {
			existing.Nickname = nil // Clear
		} else {
			trimmed := strings.TrimSpace(*req.Nickname)
			existing.Nickname = &trimmed
		}
	}

	if req.CustomColor != nil {
		if *req.CustomColor == "" {
			existing.CustomColor = nil // Clear
		} else {
			existing.CustomColor = req.CustomColor
		}
	}

	if req.CustomIcon != nil {
		if *req.CustomIcon == "" {
			existing.CustomIcon = nil // Clear
		} else {
			existing.CustomIcon = req.CustomIcon
		}
	}

	if req.CustomEmoji != nil {
		if *req.CustomEmoji == "" {
			existing.CustomEmoji = nil // Clear
		} else {
			existing.CustomEmoji = req.CustomEmoji
		}
	}

	if req.DisplayOrder != nil {
		existing.DisplayOrder = req.DisplayOrder
	}

	if req.IsHidden != nil {
		existing.IsHidden = *req.IsHidden
		// The cause travels WITH the flag (#1609). This endpoint is the user speaking, so a hide
		// set here is 'user' — the one cause a later re-link must never lift — and clearing the
		// hide clears the cause, because a visible chain has no hide to explain and a stale cause
		// would be inherited by whatever hid the row next.
		//
		// This deliberately UPGRADES a 'system' or 'unknown' cause to 'user' when the user hides
		// an already-hidden chain: they have now said it themselves, which is strictly better
		// information than either. It is never a downgrade.
		if *req.IsHidden {
			source := domain.HiddenSourceUser
			existing.HiddenSource = &source
		} else {
			existing.HiddenSource = nil
		}
	}

	if req.ExcludeInvestmentSync != nil {
		// ⚠ There IS companion state now, and this comment said there was not until #1631 M4:
		// "nothing is deleted or suppressed at ingestion, so there is nothing to reconcile" was
		// true of the read-filter-only design and is false of this one. The flag now suppresses
		// INGESTION as well (see domain.UserAccount.ExcludeInvestmentSync), so clearing it asks
		// for rows the database does not have, and the sync owes this chain a full-lookback
		// re-fetch.
		//
		// The companion write is nevertheless absent from this block, on purpose. It happens in
		// SQL, inside the single UPDATE that Step 4 below issues
		// (userAccountRepository.Update → investment_backfill_requested_at), for two reasons that
		// are both load-bearing:
		//
		//  1. ATOMICITY. Recording the obligation here — as a second repository call, or a queued
		//     job — could succeed at the flag and fail at the obligation, and the loss would be
		//     PERMANENT rather than retryable: the transition is true → false, so a retry of a
		//     write that already landed is false → false, which no transition-triggered mechanism
		//     can observe a second time. This service holds only a repository, reads outside any
		//     transaction, and the repository writes through its pool directly, so there is no
		//     pgx.Tx for a second write to join. One statement is atomic by definition.
		//  2. IT NEEDS THE OLD VALUE UNDER CONCURRENCY. The obligation arises on a TRANSITION, and
		//     Postgres evaluates the CASE against the row as it stands at UPDATE time. This
		//     function reads the value it loaded earlier (wasExcludedFromInvestmentSync above), so
		//     under two concurrent PATCHes its notion of the transition can be wrong; the
		//     statement's cannot.
		//
		// The Go-side capture is used for ONE thing only: deciding whether to nudge a sync in
		// Step 5, where being wrong costs a redundant sync or a little latency. It is never the
		// authority on whether a backfill is owed.
		//
		// So the assignment stays plain — but not because there is nothing to reconcile.
		existing.ExcludeInvestmentSync = *req.ExcludeInvestmentSync
	}

	// Step 3: Validate the updated account
	if err := existing.Validate(); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	// Step 4: Persist updates
	err = s.repo.Update(ctx, existing)
	if err != nil {
		return nil, err
	}

	// Step 5: if the owner just RE-INCLUDED a previously excluded chain, nudge a sync so the
	// backfill happens now rather than whenever Plaid next has something to say about this item.
	//
	// ⚠ Deliberately AFTER the write and deliberately unable to fail the request. The durable
	// obligation is already committed by Step 4's single statement; this only shortens the wait.
	if wasExcludedFromInvestmentSync && !existing.ExcludeInvestmentSync {
		s.nudgeInvestmentBackfill(ctx, existing.ID, userID)
	}

	return existing, nil
}

// nudgeInvestmentBackfill enqueues an investment-transactions sync for the live linked item that
// carries this chain, so a just-recorded backfill obligation is honoured promptly.
//
// ⚠ Void by design, and every failure path here is a no-op rather than an error. The obligation
// itself is already durably committed on the user_accounts row, and ANY investment-transactions
// sync for the item discharges it — this webhook-independent nudge only decides whether that
// happens in seconds or at Plaid's convenience, which for a quiet brokerage can be months. A
// returned error would therefore fail a customization the user successfully made, in exchange for
// nothing.
//
// ⚠ It does NOT decide whether a backfill is owed. Its caller's Go-side transition check can be
// wrong under two concurrent PATCHes; the authority is investment_backfill_requested_at, and the
// sync reads that, not this. A spurious nudge costs one extra sync whose window the marker's
// absence narrows back to incremental; a missed nudge costs latency only.
func (s *userAccountService) nudgeInvestmentBackfill(ctx context.Context, userAccountID, userID string) {
	if s.jobQueue == nil || s.accountRepo == nil {
		return // not wired (tests, or a deployment with no queue) — the obligation still stands
	}

	// GetByUserAccountIDAndUser prefers the ACTIVE account on the chain and is user-scoped, which
	// answers the "which linked item?" question without this code having to choose: a chain spans
	// re-links, and the item that currently carries it is the only one whose access token can fetch
	// anything. A chain whose every member is terminal still returns a row, and enqueueing for its
	// item is harmless — the sync will find nothing to store and the marker will simply stand.
	account, err := s.accountRepo.GetByUserAccountIDAndUser(ctx, userAccountID, userID)
	if err != nil || account == nil || account.LinkedItemID == "" {
		s.logger.Info("investment backfill recorded but no live account found to sync it through; "+
			"the obligation stays on the row and the next sync for this chain will honour it",
			"user_account_id", userAccountID,
			"error", err,
		)
		return
	}

	opts := []queue.EnqueueOption{
		// Same 5-minute by-args uniqueness the initial-link enqueue uses. A dedup against an
		// in-flight sync for the same item is the CORRECT outcome, not a lost nudge: that sync
		// reads the marker too.
		queue.WithUnique(*plaidSyncUniqueOpts()),
		queue.WithTags("investment_backfill"),
	}
	if _, err := s.jobQueue.Enqueue(ctx, investmentTransactionsSyncJobArgs{
		UserID: userID,
		ItemID: account.LinkedItemID,
	}, opts...); err != nil {
		s.logger.Warn("failed to enqueue the investment backfill sync; the obligation is durable "+
			"on user_accounts.investment_backfill_requested_at and the next sync will honour it",
			"user_account_id", userAccountID,
			"linked_item_id", account.LinkedItemID,
			"error", err,
		)
		return
	}
	s.logger.Info("enqueued an investment backfill sync after the owner re-included a chain",
		"user_account_id", userAccountID,
		"linked_item_id", account.LinkedItemID,
	)
}

// SetHidden updates the is_hidden flag on a UserAccount, scoped to its owner (#1566).
//
// ⚠ hidden = true is a SYSTEM hide and nothing else (#1609). The sole caller is DeleteAccount,
// hiding a chain that lost its last active account; the cause is recorded as 'system' so a later
// re-link may lift it, and an already-recorded cause is never downgraded. A hide the USER asks
// for does NOT come through here — it arrives via UpdateCustomization (PATCH
// /api/v1/user-accounts/{id}) and is recorded as 'user'. Do not repurpose this method for one:
// a user hide labelled 'system' is silently destroyed by the next re-link.
func (s *userAccountService) SetHidden(ctx context.Context, userAccountID, userID string, hidden bool) error {
	if hidden {
		return s.repo.SetHiddenBySystem(ctx, userAccountID, userID)
	}
	return s.repo.ClearHidden(ctx, userAccountID, userID)
}

// strPtrIfNotEmpty returns a pointer to s if s is not empty, otherwise nil
func strPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// derefPtr safely dereferences a pointer to string, returning empty string if nil
func derefPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
