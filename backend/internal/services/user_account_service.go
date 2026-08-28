package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
	// This is the core re-link matching logic: match on (user_id, institution_id, mask).
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
	// Returns repository.ErrUserAccountNotFound (unwrapped) if userID does not own userAccountID (#1566).
	SetHidden(ctx context.Context, userAccountID, userID string, hidden bool) error
}

// userAccountService implements UserAccountService
type userAccountService struct {
	repo repository.UserAccountRepository
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
	// Step 1: Try to find existing user account by (user_id, institution_id, mask)
	existing, err := s.repo.FindByInstitutionAndMask(ctx, userID, institutionID, mask)
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
	// (normalizeExistingUserAccount -> UpdatePrimaryTypeAndSubtype); do not conflate them. Nothing
	// reads IdentityType/IdentitySubtype for matching yet — a future task changes the match key.
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
			winner, findErr := s.repo.FindByInstitutionAndMask(ctx, userID, institutionID, mask)
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

	// Un-hide if previously hidden (re-link after account deletion).
	// Only update in-memory state on success to keep DB and caller in sync.
	if existing.IsHidden {
		err := s.repo.UpdateHidden(ctx, existing.ID, actorUserID, false)
		switch {
		case errors.Is(err, repository.ErrUserAccountNotFound):
			return nil, fmt.Errorf("user %s does not own user account %s: %w", actorUserID, existing.ID, err)
		case err != nil:
			// Non-fatal: the row stays hidden, and so does the in-memory copy.
		default:
			existing.IsHidden = false
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

	return existing, nil
}

// SetHidden updates the is_hidden flag on a UserAccount, scoped to its owner (#1566).
func (s *userAccountService) SetHidden(ctx context.Context, userAccountID, userID string, hidden bool) error {
	return s.repo.UpdateHidden(ctx, userAccountID, userID, hidden)
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
