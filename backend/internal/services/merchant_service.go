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

// BulkSetOverrides validates and applies multiple overrides atomically.
// All entries are validated before any are persisted; invalid entries fail the entire call.
// Entries are partitioned into display-only (via BulkUpsert) and merge-bearing (via BulkApply)
// with display_name threaded atomically into merge operations (G27: no staging write).
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

	// Phase 2: Read — resolve canonicals for merge groups and identify self-merges.
	// All reads happen before the atomic write so BulkApply is a pure write.
	groups := make([]repository.BulkMergeGroup, 0, len(mergeGroupOrder))
	for _, target := range mergeGroupOrder {
		sources := mergeGroups[target]
		displayNames := mergeDisplays[target]

		// Only forward displayNames if at least one entry carries a display_name.
		var dn []*string
		for _, d := range displayNames {
			if d != nil {
				dn = displayNames
				break
			}
		}

		canonicalID, err := s.resolveCanonical(ctx, userID, target, make(map[string]struct{}))
		if err != nil {
			return nil, fmt.Errorf("BulkSetOverrides: resolve canonical for target %s: %w", target, err)
		}

		// Separate self-merges (src == canonical) from real sources.
		// Self-merge renames join displayOnlyItems so they commit inside the same BulkApply tx.
		realSources := make([]string, 0, len(sources))
		var realDN []*string
		if dn != nil {
			realDN = make([]*string, 0, len(sources))
		}
		for i, src := range sources {
			if src == canonicalID {
				// Self-merge: if it carries a display_name, treat as a pure rename.
				var srcDN *string
				if dn != nil && i < len(dn) {
					srcDN = dn[i]
				}
				if srcDN != nil {
					displayOnlyItems = append(displayOnlyItems, domain.MerchantNameOverride{
						UserID:      userID,
						MerchantID:  src,
						DisplayName: srcDN,
					})
				}
				continue
			}
			realSources = append(realSources, src)
			if realDN != nil {
				var srcDN *string
				if i < len(dn) {
					srcDN = dn[i]
				}
				realDN = append(realDN, srcDN)
			}
		}

		if len(realSources) == 0 {
			continue
		}

		// Build toFlatten: sources + the intermediate target (if it was pre-merged).
		toFlatten := make([]string, 0, len(realSources)+1)
		toFlatten = append(toFlatten, realSources...)
		if canonicalID != target {
			toFlatten = append(toFlatten, target)
		}

		groups = append(groups, repository.BulkMergeGroup{
			Sources:      realSources,
			Canonical:    canonicalID,
			ToFlatten:    toFlatten,
			DisplayNames: realDN,
		})
	}

	// Phase 3: Single atomic write — display-only upserts + all merge groups in one tx.
	return s.merchantOverrideRepo.BulkApply(ctx, userID, displayOnlyItems, groups)
}

// GetMerchantOverrides returns all per-user merchant name overrides for a user,
// ordered by created_at DESC.
func (s *MerchantService) GetMerchantOverrides(ctx context.Context, userID string) ([]domain.MerchantNameOverride, error) {
	return s.merchantOverrideRepo.GetByUserID(ctx, userID)
}

// applyMergeFlatten is the shared implementation for all merge-write paths. It resolves
// the canonical (following merge chains), filters self-merges, and atomically flattens
// depth-≥2 edges via MergeAndFlatten — the single source of truth for the depth-1 invariant.
// displayNames is an optional parallel slice; when non-nil, each name is threaded atomically
// (G27: no pre-Upsert). Returns (canonicalID, results, error). When all sources are self-merges,
// results is empty and canonicalID is the resolved canonical for caller's fallback (G28).
// ErrMergeCycle is returned on cycle detection.
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

	// toFlatten: all IDs whose inbound chains must be re-pointed to canonical
	// (G20: sources may be prior canonicals; G17: intermediate target if pre-merged)
	toFlatten := make([]string, 0, len(sources)+1)
	toFlatten = append(toFlatten, sources...)
	if canonicalTargetID != targetMerchantID {
		toFlatten = append(toFlatten, targetMerchantID)
	}

	results, err := s.merchantOverrideRepo.MergeAndFlatten(ctx, userID, sources, canonicalTargetID, toFlatten, filteredDN)
	return canonicalTargetID, results, err
}

// MergeMerchants creates overrides pointing each source to the canonical target,
// maintaining the depth-1 invariant atomically. It wraps applyMergeFlatten, the shared
// implementation for all merge-write paths. Sources equal to canonical are skipped.
// ErrMergeCycle is returned on cycle detection (DB corruption).
func (s *MerchantService) MergeMerchants(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string) ([]domain.MerchantNameOverride, error) {
	if len(merchantIDs) == 0 {
		return []domain.MerchantNameOverride{}, nil
	}
	// Pass nil displayNames — MergeMerchants never carries a display_name.
	_, results, err := s.applyMergeFlatten(ctx, userID, merchantIDs, targetMerchantID, nil)
	return results, err
}

// resolveCanonical follows the merge chain until reaching a merchant with no merge pointer
// (the canonical). A visited set detects cycles; if found, ErrMergeCycle is returned.
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
