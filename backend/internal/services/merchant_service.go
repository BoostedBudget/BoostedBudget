package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/merchant"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// duplicateThreshold is the default fuzzy-similarity threshold passed to
// GetDuplicateSuggestions. Values are in [0,1]; 0.6 catches close-but-not-exact
// pairs (e.g. "Starbucks" vs "Starbucks Coffee") while avoiding false positives.
const duplicateThreshold = 0.6

// SetOverrideRequest is the input shape for a single override in BulkSetOverrides
// and the handler layer. At least one of DisplayName or MergedIntoMerchantID must
// be non-nil. Issue #957.
type SetOverrideRequest struct {
	MerchantID           string  `json:"merchant_id"`
	DisplayName          *string `json:"display_name,omitempty"`
	MergedIntoMerchantID *string `json:"merged_into_merchant_id,omitempty"`
}

// MerchantService provides business logic for merchant name management.
// It wraps MerchantRepo (read/upsert) and MerchantOverrideRepo (per-user overrides).
// Issue #957.
type MerchantService struct {
	merchantRepo         repository.MerchantRepo
	merchantOverrideRepo repository.MerchantOverrideRepo
	normalize            func(string) string
}

// NewMerchantService constructs a MerchantService with the production normalize func.
func NewMerchantService(merchantRepo repository.MerchantRepo, overrideRepo repository.MerchantOverrideRepo) *MerchantService {
	return &MerchantService{
		merchantRepo:         merchantRepo,
		merchantOverrideRepo: overrideRepo,
		normalize:            merchant.Normalize,
	}
}

// ListMerchants returns all merchants visible to the user, including effective
// display names, raw name variants, spend totals, and override flags.
func (s *MerchantService) ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error) {
	return s.merchantRepo.ListMerchants(ctx, userID)
}

// GetDuplicateSuggestions returns groups of merchants that appear to be duplicates
// of each other, using a default fuzzy-similarity threshold of 0.6.
func (s *MerchantService) GetDuplicateSuggestions(ctx context.Context, userID string) ([][]domain.MerchantSummary, error) {
	return s.merchantRepo.GetDuplicateSuggestions(ctx, userID, duplicateThreshold)
}

// SetOverride creates or updates a per-user override for a merchant. At least one of
// displayName or mergedIntoMerchantID must be non-nil; otherwise an error is returned.
// A blank (whitespace-only) display name is rejected to prevent invisible merchant names.
func (s *MerchantService) SetOverride(ctx context.Context, userID, merchantID string, displayName *string, mergedIntoMerchantID *string) (*domain.MerchantNameOverride, error) {
	if displayName == nil && mergedIntoMerchantID == nil {
		return nil, fmt.Errorf("at least one of display_name or merged_into_merchant_id must be set")
	}
	if displayName != nil {
		trimmed := strings.TrimSpace(*displayName)
		if trimmed == "" {
			return nil, fmt.Errorf("display_name must not be blank")
		}
		displayName = &trimmed
	}
	return s.merchantOverrideRepo.Upsert(ctx, domain.MerchantNameOverride{
		UserID:               userID,
		MerchantID:           merchantID,
		DisplayName:          displayName,
		MergedIntoMerchantID: mergedIntoMerchantID,
	})
}

// ClearOverride removes the per-user override for a merchant, reverting to the
// normalized name for display.
func (s *MerchantService) ClearOverride(ctx context.Context, userID, merchantID string) error {
	return s.merchantOverrideRepo.Delete(ctx, userID, merchantID)
}

// BulkSetOverrides validates and upserts multiple overrides in a single batch.
// All entries are validated before any are persisted; if any entry is invalid
// the entire call fails without writing.
func (s *MerchantService) BulkSetOverrides(ctx context.Context, userID string, reqs []SetOverrideRequest) ([]domain.MerchantNameOverride, error) {
	if len(reqs) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	overrides := make([]domain.MerchantNameOverride, 0, len(reqs))
	for _, req := range reqs {
		if req.DisplayName == nil && req.MergedIntoMerchantID == nil {
			return nil, fmt.Errorf("override for merchant_id %q: at least one of display_name or merged_into_merchant_id must be set", req.MerchantID)
		}
		displayName := req.DisplayName
		if displayName != nil {
			trimmed := strings.TrimSpace(*displayName)
			if trimmed == "" {
				return nil, fmt.Errorf("override for merchant_id %q: display_name must not be blank", req.MerchantID)
			}
			displayName = &trimmed
		}
		overrides = append(overrides, domain.MerchantNameOverride{
			UserID:               userID,
			MerchantID:           req.MerchantID,
			DisplayName:          displayName,
			MergedIntoMerchantID: req.MergedIntoMerchantID,
		})
	}
	return s.merchantOverrideRepo.BulkUpsert(ctx, overrides)
}

// GetMerchantOverrides returns all per-user merchant name overrides for a user,
// ordered by created_at DESC.
func (s *MerchantService) GetMerchantOverrides(ctx context.Context, userID string) ([]domain.MerchantNameOverride, error) {
	overrides, err := s.merchantOverrideRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if overrides != nil {
		return overrides, nil
	}
	return []domain.MerchantNameOverride{}, nil
}

// MergeMerchants creates overrides that point each source merchantID to the
// targetMerchantID. Transactions for the source merchants will display under
// the target merchant's name at read time. The target itself is excluded from
// the source list to prevent self-merge.
func (s *MerchantService) MergeMerchants(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string) ([]domain.MerchantNameOverride, error) {
	if len(merchantIDs) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	overrides := make([]domain.MerchantNameOverride, 0, len(merchantIDs))
	for _, id := range merchantIDs {
		if id == targetMerchantID {
			continue
		}
		overrides = append(overrides, domain.MerchantNameOverride{
			UserID:               userID,
			MerchantID:           id,
			MergedIntoMerchantID: &targetMerchantID,
		})
	}
	return s.merchantOverrideRepo.BulkUpsert(ctx, overrides)
}
