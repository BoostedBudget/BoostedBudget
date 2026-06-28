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
//
// When mergedIntoMerchantID is set, the write is routed through applyMergeFlatten so that
// the depth-1 chain invariant is enforced — the target is resolved to its canonical and any
// existing depth-≥2 edges are flattened atomically. This prevents MCP / direct-API callers
// from creating depth-≥2 chains that the single-hop read resolvers cannot follow (G24).
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

	// Pure rename (no merge pointer): fast path via Upsert.
	if mergedIntoMerchantID == nil {
		return s.merchantOverrideRepo.Upsert(ctx, domain.MerchantNameOverride{
			UserID:      userID,
			MerchantID:  merchantID,
			DisplayName: displayName,
		})
	}

	// Combined rename+merge: persist display_name first so MergeAndFlatten's ON CONFLICT
	// clause (which keeps existing display_name) will preserve it on the source row.
	if displayName != nil {
		if _, err := s.merchantOverrideRepo.Upsert(ctx, domain.MerchantNameOverride{
			UserID:      userID,
			MerchantID:  merchantID,
			DisplayName: displayName,
		}); err != nil {
			return nil, err
		}
	}

	// Route through the depth-1-invariant-enforcing flatten path.
	results, err := s.applyMergeFlatten(ctx, userID, []string{merchantID}, *mergedIntoMerchantID)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		// merchantID == canonical (self-merge skipped); return a minimal representation.
		return &domain.MerchantNameOverride{
			UserID:               userID,
			MerchantID:           merchantID,
			MergedIntoMerchantID: mergedIntoMerchantID,
		}, nil
	}
	return &results[0], nil
}

// ClearOverride removes the per-user override for a merchant, reverting to the
// normalized name for display.
func (s *MerchantService) ClearOverride(ctx context.Context, userID, merchantID string) error {
	return s.merchantOverrideRepo.Delete(ctx, userID, merchantID)
}

// BulkSetOverrides validates and applies multiple overrides in a single call.
// All entries are validated before any are persisted; if any entry is invalid
// the entire call fails without writing.
//
// Entries are partitioned into two groups:
//   - Display-only (no merge pointer): persisted via BulkUpsert; results included in response.
//   - Merge-bearing: if display_name is also set, the name is staged via a separate BulkUpsert
//     (so MergeAndFlatten's ON CONFLICT clause preserves it) but that staging result is NOT
//     returned — only the MergeAndFlatten result is included, preventing duplicate rows.
//     Each target group is then routed through applyMergeFlatten to enforce the depth-1 chain
//     invariant (G24), matching the guarantee provided by the dedicated mergeMerchants endpoint.
func (s *MerchantService) BulkSetOverrides(ctx context.Context, userID string, reqs []SetOverrideRequest) ([]domain.MerchantNameOverride, error) {
	if len(reqs) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	// Validate all entries before writing anything.
	for _, req := range reqs {
		if req.DisplayName == nil && req.MergedIntoMerchantID == nil {
			return nil, fmt.Errorf("override for merchant_id %q: at least one of display_name or merged_into_merchant_id must be set", req.MerchantID)
		}
		if req.DisplayName != nil && strings.TrimSpace(*req.DisplayName) == "" {
			return nil, fmt.Errorf("override for merchant_id %q: display_name must not be blank", req.MerchantID)
		}
	}

	var displayOnlyItems []domain.MerchantNameOverride      // included in response
	var displayStagingItems []domain.MerchantNameOverride  // staging for combined entries, not in response
	mergeGroups := make(map[string][]string)               // target → source IDs (first-seen order)
	var mergeGroupOrder []string                           // targets in order encountered

	for _, req := range reqs {
		dn := req.DisplayName
		if dn != nil {
			trimmed := strings.TrimSpace(*dn)
			dn = &trimmed
		}
		if req.MergedIntoMerchantID == nil {
			displayOnlyItems = append(displayOnlyItems, domain.MerchantNameOverride{
				UserID:      userID,
				MerchantID:  req.MerchantID,
				DisplayName: dn,
			})
			continue
		}
		// Merge-bearing entry: stage display_name for BulkUpsert so MergeAndFlatten's
		// ON CONFLICT clause can preserve it. MergeAndFlatten's result is authoritative
		// (prevents duplicate rows in response).
		if dn != nil {
			displayStagingItems = append(displayStagingItems, domain.MerchantNameOverride{
				UserID:      userID,
				MerchantID:  req.MerchantID,
				DisplayName: dn,
			})
		}
		target := *req.MergedIntoMerchantID
		if _, seen := mergeGroups[target]; !seen {
			mergeGroupOrder = append(mergeGroupOrder, target)
		}
		mergeGroups[target] = append(mergeGroups[target], req.MerchantID)
	}

	results := make([]domain.MerchantNameOverride, 0, len(reqs))

	// BulkUpsert display-only entries (included in response).
	if len(displayOnlyItems) > 0 {
		bulkResults, err := s.merchantOverrideRepo.BulkUpsert(ctx, displayOnlyItems)
		if err != nil {
			return nil, err
		}
		results = append(results, bulkResults...)
	}

	// Stage display_name for combined entries so MergeAndFlatten's ON CONFLICT
	// can preserve it. Results discarded (MergeAndFlatten is authoritative).
	if len(displayStagingItems) > 0 {
		if _, err := s.merchantOverrideRepo.BulkUpsert(ctx, displayStagingItems); err != nil {
			return nil, err
		}
	}

	for _, target := range mergeGroupOrder {
		merged, err := s.applyMergeFlatten(ctx, userID, mergeGroups[target], target)
		if err != nil {
			return nil, err
		}
		results = append(results, merged...)
	}

	return results, nil
}

// GetMerchantOverrides returns all per-user merchant name overrides for a user,
// ordered by created_at DESC.
func (s *MerchantService) GetMerchantOverrides(ctx context.Context, userID string) ([]domain.MerchantNameOverride, error) {
	return s.merchantOverrideRepo.GetByUserID(ctx, userID)
}

// applyMergeFlatten is the shared implementation for all merge-write paths. It resolves
// targetMerchantID to its canonical (following any existing merge chain), filters out
// self-merges, and atomically flattens depth-≥2 edges + upserts source→canonical overrides
// via MergeAndFlatten. This is the single source of truth for the depth-1 chain invariant.
//
// Used by: MergeMerchants, SetOverride (when mergedInto != nil), and BulkSetOverrides
// (merge-bearing entries). ErrMergeCycle is returned on cycle detection.
func (s *MerchantService) applyMergeFlatten(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string) ([]domain.MerchantNameOverride, error) {
	if len(merchantIDs) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	// Resolve the canonical target, following any existing merge chain.
	canonicalTargetID, err := s.resolveCanonical(ctx, userID, targetMerchantID, make(map[string]struct{}))
	if err != nil {
		return nil, fmt.Errorf("applyMergeFlatten: resolve canonical: %w", err)
	}

	// Filter sources: skip any that equal the canonical (self-merge).
	sources := make([]string, 0, len(merchantIDs))
	for _, id := range merchantIDs {
		if id != canonicalTargetID {
			sources = append(sources, id)
		}
	}
	if len(sources) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}

	// toFlatten = all IDs whose inbound chains must be re-pointed to canonical:
	//   • all sources (G20: a source may itself be a prior canonical with inbound chains)
	//   • the intermediate target if the target was pre-merged (G17: existing pointers at the
	//     old target must also be re-pointed)
	toFlatten := make([]string, 0, len(sources)+1)
	toFlatten = append(toFlatten, sources...)
	if canonicalTargetID != targetMerchantID {
		toFlatten = append(toFlatten, targetMerchantID)
	}

	return s.merchantOverrideRepo.MergeAndFlatten(ctx, userID, sources, canonicalTargetID, toFlatten)
}

// MergeMerchants creates overrides that point each source merchantID to the canonical target,
// maintaining the depth-1 chain invariant atomically. It is a thin wrapper around
// applyMergeFlatten, which is the shared implementation used by all merge-write paths
// (including SetOverride and BulkSetOverrides with merge pointers).
//
// Sources equal to the canonical target are skipped. An ErrMergeCycle is returned if the
// target's existing chain contains a cycle, indicating DB corruption.
func (s *MerchantService) MergeMerchants(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string) ([]domain.MerchantNameOverride, error) {
	if len(merchantIDs) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}
	return s.applyMergeFlatten(ctx, userID, merchantIDs, targetMerchantID)
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
