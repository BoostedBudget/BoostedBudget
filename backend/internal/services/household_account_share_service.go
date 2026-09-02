package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// HouseholdAccountShareService is a narrow interface for managing per-account
// household-visibility grants, kept separate from the broad HouseholdService
// interface so adding it doesn't require updating every existing hand-written
// mock of HouseholdService (e.g. operations/household_test.go's
// mockHouseholdService). *householdService implements this interface alongside
// HouseholdService. #1368 — Phase 4: account sharing grants.
type HouseholdAccountShareService interface {
	// SetAccountShareVisibility toggles the caller's own user_account's household
	// share visibility. Active-member-only (returns domain.ErrHouseholdNotFound if
	// the caller has no active membership) and owner-of-account-only (returns
	// domain.ErrAccountNotOwned if userAccountID does not belong to the caller).
	// The caller always ends up with exactly one OPEN grant (revoked_at NULL) carrying
	// the requested visibility — including 'hidden', so a later member removal still
	// finds and revokes it.
	//
	// #1569 T7 corrected this contract, which used to read "Never writes revoked_at —
	// that is reserved for member removal (RevokeSharesForUser)". It does now: a
	// visibility CHANGE closes the outgoing state's interval (stamping its revoked_at)
	// and opens a new one, because overwriting the single row in place rewrote the
	// account's whole history to whatever the member last chose. Removal is still the
	// only thing that leaves the account with NO open grant.
	//
	// Runs in one transaction that locks the household row and then the caller's own
	// active household_members row — in that order, per this codebase's lock-ordering
	// convention, which the interval INSERT above made load-bearing (see the
	// implementation's comment for the deadlock it fixes). It therefore cannot race a
	// concurrent RemoveMember/self-leave on either row.
	SetAccountShareVisibility(ctx context.Context, callerUserID, userAccountID string, visibility domain.ShareVisibility) (*domain.HouseholdAccountShare, error)

	// ListMyAccountShares returns the caller's own accounts with their current
	// household-share state (nil Visibility means no explicit grant yet, e.g. an
	// account linked after the member joined). Returns domain.ErrHouseholdNotFound
	// if the caller has no active membership.
	ListMyAccountShares(ctx context.Context, callerUserID string) ([]domain.UserAccountShareView, error)
}

func (s *householdService) SetAccountShareVisibility(ctx context.Context, callerUserID, userAccountID string, visibility domain.ShareVisibility) (*domain.HouseholdAccountShare, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7 (household_service.go).
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	shareRepo := repository.NewHouseholdShareRepository(tx)

	// Household-row lock FIRST, per this codebase's lock-ordering convention (RemoveMember,
	// OffboardExcessMembers, ForceDisband and PurgeRemovedMembership all take it before
	// touching household_members).
	//
	// #1569 T7 is what forced this method to join them. It used to lock only the member row,
	// and got away with the inversion because SetShareVisibility's single INSERT ... ON
	// CONFLICT always resolved to the DO UPDATE branch: an update that leaves household_id
	// untouched fires no FK trigger, so it never took the KEY SHARE lock on households that a
	// real insert does. Now that a visibility change closes one interval and INSERTS the next,
	// that lock IS taken — and a concurrent RemoveMember (households FOR UPDATE, then this
	// member's household_members row) deadlocked against it reproducibly, 2 runs in 8 under
	// TestHouseholdServiceIntegration_SetAccountShareVisibility_RaceRemoveMember_NoActiveShareSurvives.
	//
	// resolveHousehold is a non-locking lookup used only to learn WHICH household to lock; the
	// authoritative membership check is still LockActiveMembershipForUser below, so a caller
	// with no active membership still gets ErrHouseholdNotFound and the #1368 Codex High-4
	// serialization against a concurrent removal is unchanged.
	resolved, _, found, cerr := hhRepo.GetHouseholdForUser(ctx, callerUserID)
	if cerr != nil {
		return nil, cerr
	}
	if !found {
		cerr = domain.ErrHouseholdNotFound
		return nil, cerr
	}
	if _, cerr = hhRepo.LockHouseholdByID(ctx, resolved.ID); cerr != nil {
		return nil, cerr
	}

	// Lock the caller's own active household_members row for the remainder of this
	// transaction. This serializes against RemoveMember (self-leave or owner-removal),
	// which updates that same row — so either we observe 404 after a removal already
	// committed, or a concurrent removal blocks until we commit/rollback and then
	// revokes the row we just wrote via RevokeSharesForUser. #1368 Codex High-4.
	household, cerr := hhRepo.LockActiveMembershipForUser(ctx, callerUserID)
	if cerr != nil {
		return nil, cerr
	}

	// Ownership check: the caller may only set visibility on their own user_accounts.
	fingerprints, cerr := shareRepo.ListActiveUserAccountFingerprints(ctx, callerUserID)
	if cerr != nil {
		return nil, cerr
	}
	var physicalBlindIdx *string
	owned := false
	for _, fp := range fingerprints {
		if fp.UserAccountID == userAccountID {
			owned = true
			physicalBlindIdx = fp.PhysicalBlindIdx
			break
		}
	}
	if !owned {
		cerr = domain.ErrAccountNotOwned
		return nil, cerr
	}

	share, cerr := shareRepo.SetShareVisibility(ctx, household.ID, userAccountID, physicalBlindIdx, visibility)
	if cerr != nil {
		return nil, cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return nil, cerr
	}

	if visibility == domain.ShareVisibilityHidden {
		s.capture(callerUserID, phclient.EventHouseholdAccountHidden)
	} else {
		s.capture(callerUserID, phclient.EventHouseholdAccountShared)
	}
	// Issue #1531 Task 10b-2b: a share visibility change alters what OTHER household members
	// see in their own combined/shared-account analytics, not just the caller's — the HTTP
	// mutation middleware (Task 10b-1) only bumps the acting caller (and household scope),
	// never the other members this write actually affects. Queried via s.householdRepo (the
	// plain, POOL-bound field, never the tx-scoped hhRepo local above) and only AFTER commit,
	// so no new query needs to be expected inside the transaction (including on any
	// validation-failure path above, which all return before reaching this point).
	s.bumpCacheForHousehold(ctx, household.ID)
	s.bumpCacheForCurrentMembers(ctx, household.ID)
	return share, nil
}

func (s *householdService) ListMyAccountShares(ctx context.Context, callerUserID string) ([]domain.UserAccountShareView, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7 (household_service.go).
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	shareRepo := repository.NewHouseholdShareRepository(tx)

	// Same active-membership lock as SetAccountShareVisibility (#1368 Codex
	// implementation review, Low-1): without it, a concurrent RemoveMember could
	// revoke the caller's membership between this check and the share query below,
	// silently returning 200 for a member who was just removed. Locking first makes
	// the two reads a single atomic "am I still active?" snapshot.
	household, cerr := hhRepo.LockActiveMembershipForUser(ctx, callerUserID)
	if cerr != nil {
		return nil, cerr
	}

	views, cerr := shareRepo.ListMyAccountShares(ctx, household.ID, callerUserID)
	if cerr != nil {
		return nil, cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return nil, cerr
	}
	return views, nil
}
