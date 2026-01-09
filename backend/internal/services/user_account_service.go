package services

import (
	"context"
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
	MatchOrCreate(ctx context.Context, userID, institutionID string, mask *string, accountName, accountType, accountSubtype string) (*domain.UserAccount, error)

	// GetByUserID retrieves all user accounts for a user.
	// If includeHidden is false, filters out accounts where is_hidden=true.
	GetByUserID(ctx context.Context, userID string, includeHidden bool) ([]domain.UserAccount, error)

	// GetByID retrieves a user account by ID with ownership validation.
	GetByID(ctx context.Context, id, userID string) (*domain.UserAccount, error)

	// UpdateCustomization updates user account customization fields.
	// Returns the updated UserAccount.
	UpdateCustomization(ctx context.Context, id, userID string, req *domain.UpdateUserAccountRequest) (*domain.UserAccount, error)
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
		// Check if type/subtype needs updating
		existingType := ""
		existingSubtype := ""
		if existing.PrimaryType != nil {
			existingType = *existing.PrimaryType
		}
		if existing.PrimarySubtype != nil {
			existingSubtype = *existing.PrimarySubtype
		}

		if existingType != accountType || existingSubtype != accountSubtype {
			// Update type/subtype from new Plaid data
			err = s.repo.UpdatePrimaryTypeAndSubtype(ctx, existing.ID, accountType, accountSubtype)
			if err != nil {
				// Non-fatal: log and continue (customizations preserved)
				// In production, would log warning here
			}
		}

		// Return existing account (customizations preserved)
		return existing, nil
	}

	// Step 2: No match found - create new UserAccount
	newUA := &domain.UserAccount{
		UserID:          userID,
		InstitutionID:   institutionID,
		Mask:            mask,
		AccountNameHint: strPtrIfNotEmpty(accountName),
		PrimaryType:     strPtrIfNotEmpty(accountType),
		PrimarySubtype:  strPtrIfNotEmpty(accountSubtype),
		IsHidden:        false, // Default to visible
	}

	err = s.repo.Create(ctx, newUA)
	if err != nil {
		return nil, err
	}

	return newUA, nil
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

// strPtrIfNotEmpty returns a pointer to s if s is not empty, otherwise nil
func strPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
