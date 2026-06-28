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
//
// G27: when both displayName and mergedIntoMerchantID are set, the display_name is threaded
// into MergeAndFlatten atomically (no pre-Upsert). A separate pre-Upsert with nil
// merged_into would transiently clear an existing merge pointer before the flatten write — if
// the flatten then fails, the row is left persistently unmerged.
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

	// Merge (with optional rename): thread displayName into applyMergeFlatten so both the
	// display write and the flatten happen inside a single transaction (G27 atomic fix).
	// No pre-Upsert — avoids the transient NULL of an existing merge pointer.
	var dn []*string
	if displayName != nil {
		dn = []*string{displayName}
	}
	canonicalID, results, err := s.applyMergeFlatten(ctx, userID, []string{merchantID}, *mergedIntoMerchantID, dn)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		// merchantID == canonical (self-merge skipped; no merge write occurred).
		// G27 self-merge fallback: persist display_name if provided (the merge write was
		// skipped entirely, so we must write the rename separately).
		if displayName != nil {
			return s.merchantOverrideRepo.Upsert(ctx, domain.MerchantNameOverride{
				UserID:      userID,
				MerchantID:  merchantID,
				DisplayName: displayName,
			})
		}
		// G28: return the resolved canonical ID, not the raw request value.
		return &domain.MerchantNameOverride{
			UserID:               userID,
			MerchantID:           merchantID,
			MergedIntoMerchantID: &canonicalID,
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
//   - Merge-bearing: display_name (if present) is threaded atomically into MergeAndFlatten
//     via the displayNames parallel array (G27: no staging BulkUpsert). Only the
//     MergeAndFlatten result is included in the response, preventing duplicate rows.
//     Each target group is routed through applyMergeFlatten to enforce the depth-1 chain
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

	var displayOnlyItems []domain.MerchantNameOverride // included in response
	mergeGroups := make(map[string][]string)           // target → source IDs (first-seen order)
	mergeDisplays := make(map[string][]*string)        // target → per-source display names (G27)
	var mergeGroupOrder []string                       // targets in order encountered

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
		// Merge-bearing entry: track the source and its optional display_name.
		// G27: no staging BulkUpsert — display_name is threaded into MergeAndFlatten atomically.
		target := *req.MergedIntoMerchantID
		if _, seen := mergeGroups[target]; !seen {
			mergeGroupOrder = append(mergeGroupOrder, target)
		}
		mergeGroups[target] = append(mergeGroups[target], req.MerchantID)
		mergeDisplays[target] = append(mergeDisplays[target], dn)
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

	// Route each merge group through applyMergeFlatten (depth-1 invariant + G27 atomic display).
	for _, target := range mergeGroupOrder {
		sources := mergeGroups[target]
		displayNames := mergeDisplays[target]
		// Only pass displayNames if at least one entry has a display_name to set;
		// otherwise pass nil so MergeAndFlatten preserves all existing display values.
		var dn []*string
		for _, d := range displayNames {
			if d != nil {
				dn = displayNames
				break
			}
		}
		canonicalID, merged, err := s.applyMergeFlatten(ctx, userID, sources, target, dn)
		if err != nil {
			return nil, err
		}
		results = append(results, merged...)

		// Self-merge fallback: any source equal to canonicalID was filtered out of the merge write.
		// If it carried a display_name, persist the rename separately (mirrors SetOverride's G28 fallback).
		for i, src := range sources {
			if src != canonicalID {
				continue
			}
			var srcDN *string
			if i < len(displayNames) {
				srcDN = displayNames[i]
			}
			if srcDN == nil {
				continue
			}
			upserted, uErr := s.merchantOverrideRepo.Upsert(ctx, domain.MerchantNameOverride{
				UserID:      userID,
				MerchantID:  src,
				DisplayName: srcDN,
			})
			if uErr != nil {
				return nil, uErr
			}
			results = append(results, *upserted)
		}
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
// displayNames is an optional parallel slice aligned with merchantIDs. When non-nil, each
// entry's display name is threaded into MergeAndFlatten atomically (G27: no pre-Upsert).
// Pass nil when no display_name change is needed (MergeMerchants path).
//
// Returns (canonicalTargetID, results, error). When all sources equal the canonical (self-merge),
// results is empty and canonicalTargetID is the resolved canonical — the caller can use it to
// handle the self-merge+rename fallback (G28).
//
// Used by: MergeMerchants, SetOverride (when mergedInto != nil), and BulkSetOverrides
// (merge-bearing entries). ErrMergeCycle is returned on cycle detection.
func (s *MerchantService) applyMergeFlatten(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string, displayNames []*string) (string, []domain.MerchantNameOverride, error) {
	if len(merchantIDs) == 0 {
		return "", []domain.MerchantNameOverride{}, nil
	}

	// Resolve the canonical target, following any existing merge chain.
	canonicalTargetID, err := s.resolveCanonical(ctx, userID, targetMerchantID, make(map[string]struct{}))
	if err != nil {
		return "", nil, fmt.Errorf("applyMergeFlatten: resolve canonical: %w", err)
	}

	// Filter sources: skip any that equal the canonical (self-merge). Maintain the
	// parallel displayNames slice so indices stay aligned with sources.
	sources := make([]string, 0, len(merchantIDs))
	var filteredDN []*string
	if len(displayNames) > 0 {
		filteredDN = make([]*string, 0, len(merchantIDs))
	}
	for i, id := range merchantIDs {
		if id != canonicalTargetID {
			sources = append(sources, id)
			if filteredDN != nil {
				if i < len(displayNames) {
					filteredDN = append(filteredDN, displayNames[i])
				} else {
					filteredDN = append(filteredDN, nil)
				}
			}
		}
	}
	if len(sources) == 0 {
		// All sources were self-merges — return canonical so caller can handle display fallback.
		return canonicalTargetID, []domain.MerchantNameOverride{}, nil
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

	results, err := s.merchantOverrideRepo.MergeAndFlatten(ctx, userID, sources, canonicalTargetID, toFlatten, filteredDN)
	return canonicalTargetID, results, err
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
	// Pass nil displayNames — MergeMerchants never carries a display_name.
	_, results, err := s.applyMergeFlatten(ctx, userID, merchantIDs, targetMerchantID, nil)
	return results, err
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
