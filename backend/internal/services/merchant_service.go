package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/merchant"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ErrMergeCycle is returned by MergeMerchants when the merge chain for the
// target merchant contains a cycle, indicating data corruption in the DB.
var ErrMergeCycle = errors.New("merchant merge cycle detected")

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
	return s.merchantOverrideRepo.GetByUserID(ctx, userID)
}

// MergeMerchants creates overrides that point each source merchantID to the
// canonical target (following any existing merge chain so all pointers stay at
// depth-1). Sources equal to the canonical target are skipped. If the target
// itself is already merged into another merchant, existing overrides pointing at
// the old target are re-flattened to the canonical as well.
func (s *MerchantService) MergeMerchants(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string) ([]domain.MerchantNameOverride, error) {
	if len(merchantIDs) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	// Resolve the canonical target, following any existing merge chain.
	canonicalTargetID, err := s.resolveCanonical(ctx, userID, targetMerchantID, make(map[string]struct{}))
	if err != nil {
		return nil, fmt.Errorf("MergeMerchants: resolve canonical: %w", err)
	}

	// If the target was itself already merged, re-flatten any existing overrides
	// pointing at the intermediate target to keep depth-1 invariant.
	if canonicalTargetID != targetMerchantID {
		if err := s.merchantOverrideRepo.ReassignMergeTarget(ctx, userID, targetMerchantID, canonicalTargetID); err != nil {
			return nil, fmt.Errorf("MergeMerchants: reassign existing chains: %w", err)
		}
	}

	overrides := make([]domain.MerchantNameOverride, 0, len(merchantIDs))
	for _, id := range merchantIDs {
		if id == canonicalTargetID {
			continue // self-merge — skip
		}
		overrides = append(overrides, domain.MerchantNameOverride{
			UserID:               userID,
			MerchantID:           id,
			MergedIntoMerchantID: &canonicalTargetID,
		})
	}
	if len(overrides) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}
	return s.merchantOverrideRepo.BulkUpsert(ctx, overrides)
}

// resolveCanonical follows the merge chain for merchantID until reaching a merchant
// with no merge pointer (the canonical). A visited set detects cycles; if a cycle
// is found, ErrMergeCycle is returned so the caller can surface the data corruption
// rather than silently treating it as a valid canonical.
func (s *MerchantService) resolveCanonical(ctx context.Context, userID, merchantID string, visited map[string]struct{}) (string, error) {
	if _, seen := visited[merchantID]; seen {
		return "", ErrMergeCycle
	}
	visited[merchantID] = struct{}{}
	target, err := s.merchantOverrideRepo.GetMergeTarget(ctx, userID, merchantID)
	if err != nil {
		return "", err
	}
	if target == "" {
		return merchantID, nil // no merge pointer — this is canonical
	}
	return s.resolveCanonical(ctx, userID, target, visited)
}
